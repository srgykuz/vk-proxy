package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	methodMessage int = iota + 1
	methodPost
	methodComment
	methodDoc
	methodQR
)

var (
	errSessionClosed    = errors.New("session is closed")
	errSessionQueueFull = errors.New("session queue is full")
)

var methodsMaxLenEncoded = map[int]int{}
var methodsMaxLenPayload = map[int]int{}

func initSession(cfg config) error {
	methodsMaxLenEncoded = map[int]int{
		methodMessage: 4096,
		methodPost:    16000,
		methodComment: 16000,
		methodDoc:     1 * 1024 * 1024,
		methodQR:      qrMaxLen[qrLevel(cfg.QR.ImageLevel)],
	}
	methodsMaxLenPayload = map[int]int{
		methodMessage: datagramCalcMaxLen(methodsMaxLenEncoded[methodMessage] - datagramHeaderLenEncoded),
		methodPost:    datagramCalcMaxLen(methodsMaxLenEncoded[methodPost] - datagramHeaderLenEncoded),
		methodComment: datagramCalcMaxLen(methodsMaxLenEncoded[methodComment] - datagramHeaderLenEncoded),
		methodDoc:     datagramCalcMaxLen(methodsMaxLenEncoded[methodDoc] - datagramHeaderLenEncoded),
		methodQR:      datagramCalcMaxLen(methodsMaxLenEncoded[methodQR] - datagramHeaderLenEncoded),
	}

	return nil
}

var sessions map[dgSes]*session = map[dgSes]*session{}
var sessionsMu sync.Mutex = sync.Mutex{}

func getSession(id dgSes) (*session, bool) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	ses, exists := sessions[id]

	return ses, exists
}

func setSession(id dgSes, ses *session) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	sessions[id] = ses
}

var sessionID dgSes = 0
var sessionIDMu sync.Mutex = sync.Mutex{}

func nextSessionID() dgSes {
	sessionIDMu.Lock()
	defer sessionIDMu.Unlock()

	sessionID++

	return sessionID
}

type session struct {
	cfg       config
	id        dgSes
	number    dgNum
	mu        sync.Mutex
	wg        sync.WaitGroup
	peer      net.Conn
	closed    bool
	onClose   chan struct{}
	history   map[dgNum]datagram
	writes    chan []byte
	datagrams chan datagram
	activity  time.Time
	post      wallPostResponse
}

func openSession(id dgSes, cfg config) (*session, error) {
	slog.Debug("session: open", "id", id)

	s := &session{
		cfg:       cfg,
		id:        id,
		number:    0,
		mu:        sync.Mutex{},
		wg:        sync.WaitGroup{},
		peer:      nil,
		closed:    false,
		onClose:   make(chan struct{}),
		history:   make(map[dgNum]datagram),
		writes:    make(chan []byte, cfg.Session.QueueSize),
		datagrams: make(chan datagram, cfg.Session.QueueSize),
		activity:  time.Now(),
		post:      wallPostResponse{},
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.listenWrites()
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.listenDatagrams()
	}()

	return s, nil
}

func (s *session) String() string {
	return fmt.Sprint(s.id)
}

func (s *session) close() {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return
	}

	if s.peer == nil {
		slog.Debug("session: close", "id", s.id)
	} else {
		slog.Debug("session: close", "id", s.id, "peer", s.peer.RemoteAddr().String())
	}

	s.closed = true

	close(s.writes)
	close(s.datagrams)

	s.mu.Unlock()

	s.wg.Wait()

	s.mu.Lock()

	if s.peer != nil {
		s.peer.Close()
	}

	close(s.onClose)

	s.mu.Unlock()
}

func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closed
}

func (s *session) isInactive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}

	return time.Since(s.activity) > s.cfg.Session.Timeout()
}

func (s *session) nextNumber() dgNum {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.number++

	return s.number
}

func (s *session) setPeer(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.peer = conn
}

func (s *session) getHistory(number dgNum) (datagram, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dg, exists := s.history[number]

	return dg, exists
}

func (s *session) writePeer(b []byte) error {
	clone := bytes.Clone(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}

	if s.peer == nil {
		return errors.New("peer is nil")
	}

	s.activity = time.Now()

	select {
	case s.writes <- clone:
		return nil
	default:
		return errSessionQueueFull
	}
}

func (s *session) listenWrites() {
	for data := range s.writes {
		if err := writeSocks(s.cfg, s, data); err != nil {
			slog.Error("session: write", "id", s.id, "err", err)
		}
	}
}

func (s *session) sendDatagram(dg datagram) error {
	clone := dg.clone()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}

	if clone.session == 0 {
		clone.session = s.id
	}

	s.activity = time.Now()

	select {
	case s.datagrams <- clone:
		return nil
	default:
		return errSessionQueueFull
	}
}

func (s *session) listenDatagrams() {
	for dg := range s.datagrams {
		methods, fragments, err := s.createPlan(dg)

		if err != nil {
			slog.Error("session: plan", "id", s.id, "dg", dg, "err", err)
			continue
		}

		s.mu.Lock()

		for _, fg := range fragments {
			s.history[fg.number] = fg
		}

		s.mu.Unlock()

		if err := s.executePlan(methods, fragments); err != nil {
			slog.Error("session: plan", "id", s.id, "dg", dg, "err", err)
		}
	}
}

func (s *session) createPlan(dg datagram) ([]int, []datagram, error) {
	initMethods := []int{methodPost, methodQR}
	forwardMethods := []int{methodDoc, methodQR}

	s.mu.Lock()

	if s.post.PostID > 0 {
		initMethods = append(initMethods, methodComment)
	}

	s.mu.Unlock()

	if rand.Int31()%3 == 0 {
		initMethods = append(initMethods, methodMessage)
	}

	methods := []int{}
	fragments := []datagram{}

	if dg.command != commandForward {
		if dg.number == 0 {
			dg.number = s.nextNumber()
		}

		n := rand.Int31n(int32(len(initMethods)))
		methods = append(methods, initMethods[n])
		fragments = append(fragments, dg)

		return methods, fragments, nil
	}

	if dg.number != 0 {
		availableMethods := []int{}

		for _, m := range forwardMethods {
			if dg.LenEncoded() <= methodsMaxLenEncoded[m] {
				availableMethods = append(availableMethods, m)
			}
		}

		if len(availableMethods) == 0 {
			return nil, nil, errors.New("no methods available")
		}

		n := rand.Int31n(int32(len(availableMethods)))
		methods = append(methods, availableMethods[n])
		fragments = append(fragments, dg)

		return methods, fragments, nil
	}

	for len(dg.payload) > 0 {
		n := rand.Int31n(int32(len(forwardMethods)))
		method := forwardMethods[n]
		chunks := bytesToChunks(dg.payload, methodsMaxLenPayload[method], 2)

		if len(chunks) == 0 || len(chunks) > 2 {
			return nil, nil, errors.New("unexpected chunks logic")
		}

		if len(chunks) == 2 {
			dg.payload = chunks[1]
		} else {
			dg.payload = nil
		}

		num := s.nextNumber()
		fg := newDatagram(dg.session, num, dg.command, chunks[0])

		if fg.LenEncoded() > methodsMaxLenEncoded[method] {
			return nil, nil, errors.New("unexpected payload logic")
		}

		methods = append(methods, method)
		fragments = append(fragments, fg)

		if len(methods) > 1000 {
			return nil, nil, errors.New("infinite loop protection")
		}
	}

	return methods, fragments, nil
}

func (s *session) executePlan(methods []int, fragments []datagram) error {
	if len(methods) != len(fragments) {
		return errors.New("methods and fragments mismatch")
	}

	qrs := []datagram{}

	for i, method := range methods {
		fg := fragments[i]

		if method == methodQR {
			qrs = append(qrs, fg)
			continue
		}

		var f func(string) error

		switch method {
		case methodMessage:
			f = s.executeMethodMessage
		case methodPost:
			f = s.executeMethodPost
		case methodComment:
			f = s.executeMethodComment
		case methodDoc:
			f = s.executeMethodDoc
		default:
			return fmt.Errorf("unknown method: %v", method)
		}

		encoded := encodeDatagram(fg)
		slog.Debug("session: send", "id", s.id, "method", method, "dg", fg)

		s.wg.Add(1)
		go func(method int) {
			defer s.wg.Done()

			if err := f(encoded); err != nil {
				slog.Error("session: send", "id", s.id, "method", method, "dg", fg, "err", err)
			}
		}(method)
	}

	if len(qrs) > 0 {
		encoded := make([]string, len(qrs))

		for i, fg := range qrs {
			encoded[i] = encodeDatagram(fg)
			slog.Debug("session: send", "id", s.id, "method", methodQR, "dg", fg)
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()

			if err := s.executeMethodQR(encoded, ""); err != nil {
				slog.Error("session: send", "id", s.id, "method", methodQR, "err", err)
			}
		}()
	}

	return nil
}

func (s *session) executeMethodMessage(encoded string) error {
	p := messagesSendParams{
		message: encoded,
	}
	_, err := messagesSend(s.cfg.API, p)

	return err
}

func (s *session) executeMethodPost(encoded string) error {
	p := wallPostParams{
		message: encoded,
	}
	resp, err := wallPost(s.cfg.API, p)

	if err != nil {
		return err
	}

	s.mu.Lock()
	s.post = resp
	s.mu.Unlock()

	return nil
}

func (s *session) executeMethodComment(encoded string) error {
	s.mu.Lock()
	post := s.post
	s.mu.Unlock()

	if post.PostID == 0 {
		return errors.New("post is not created")
	}

	p := wallCreateCommentParams{
		postID:  post.PostID,
		message: encoded,
	}
	_, err := wallCreateComment(s.cfg.API, p)

	return err
}

func (s *session) executeMethodDoc(encoded string) error {
	uploadP := docsUploadParams{
		data: []byte(encoded),
	}
	resp, err := docsUploadAndSave(s.cfg.API, uploadP)

	if err != nil {
		return err
	}

	zero := encodeDatagram(newDatagram(0, 0, 0, nil))
	arg := "caption=" + url.QueryEscape(zero)
	uri := resp.Doc.URL

	if strings.Contains(uri, "?") {
		uri += "&" + arg
	} else {
		uri += "?" + arg
	}

	msg := strings.ReplaceAll(uri, ".", ". ")
	methods := []int{methodPost, methodQR}

	s.mu.Lock()

	if s.post.PostID > 0 {
		methods = append(methods, methodComment)
	}

	s.mu.Unlock()

	n := rand.Int31n(int32(len(methods)))
	method := methods[n]

	switch method {
	case methodPost:
		err = s.executeMethodPost(msg)
	case methodComment:
		err = s.executeMethodComment(msg)
	case methodQR:
		err = s.executeMethodQR([]string{zero}, msg)
	default:
		return fmt.Errorf("unknown method: %v", method)
	}

	return err
}

func (s *session) executeMethodQR(encoded []string, caption string) error {
	qrs := make([][]byte, len(encoded))

	for i, enc := range encoded {
		qr, err := encodeQR(s.cfg.QR, enc)

		if err != nil {
			return fmt.Errorf("encode: %v", err)
		}

		qrs[i] = qr
	}

	qr, err := mergeQR(s.cfg.QR, qrs)

	if err != nil {
		return fmt.Errorf("merge: %v", err)
	}

	if len(caption) == 0 {
		zero := encodeDatagram(newDatagram(0, 0, 0, nil))
		caption = zero
	}

	p := photosUploadAndSaveParams{
		photosUploadParams: photosUploadParams{
			data: qr,
		},
		photosSaveParams: photosSaveParams{
			caption: caption,
		},
	}

	if _, err := photosUploadAndSave(s.cfg.API, p); err != nil {
		return fmt.Errorf("upload: %v", err)
	}

	return nil
}

func clearSession() error {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			time.Sleep(10 * time.Second)

			sessionsMu.Lock()

			for id, ses := range sessions {
				if ses.isInactive() {
					slog.Error("session: timeout", "id", id)

					wg.Add(1)
					go func(ses *session) {
						defer wg.Done()
						ses.close()
					}(ses)
				}
			}

			sessionsMu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			time.Sleep(5 * time.Minute)

			sessionsMu.Lock()

			for id, ses := range sessions {
				if ses.isClosed() {
					delete(sessions, id)
				}
			}

			sessionsMu.Unlock()
		}
	}()

	wg.Wait()

	return nil
}

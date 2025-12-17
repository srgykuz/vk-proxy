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
	methodStorage
	methodDescription
	methodWebsite
	methodVideoComment
	methodPhotoComment
)

var (
	errSessionClosed    = errors.New("session is closed")
	errSessionQueueFull = errors.New("session queue is full")
)

var methodsEnabled = map[int]bool{}
var methodsEncoding = map[int]int{}
var methodsMaxLenEncoded = map[int]int{}
var methodsMaxLenPayload = map[int]int{}

func initSession(cfg config) error {
	methodsEnabled = map[int]bool{
		methodMessage:      true,
		methodPost:         true,
		methodComment:      true,
		methodDoc:          true,
		methodQR:           !cfg.API.Unathorized,
		methodStorage:      true,
		methodDescription:  true,
		methodWebsite:      true,
		methodVideoComment: !cfg.API.Unathorized,
		methodPhotoComment: !cfg.API.Unathorized,
	}
	methodsEncoding = map[int]int{
		methodMessage:      datagramEncodingRU,
		methodPost:         datagramEncodingRU,
		methodComment:      datagramEncodingRU,
		methodDoc:          datagramEncodingASCII,
		methodQR:           datagramEncodingASCII,
		methodStorage:      datagramEncodingASCII,
		methodDescription:  datagramEncodingASCII,
		methodWebsite:      datagramEncodingASCII,
		methodVideoComment: datagramEncodingRU,
		methodPhotoComment: datagramEncodingRU,
	}
	methodsMaxLenEncoded = map[int]int{
		methodMessage:      4096,
		methodPost:         16000,
		methodComment:      16000,
		methodDoc:          1 * 1024 * 1024,
		methodQR:           qrMaxLen[qrLevel(cfg.QR.ImageLevel)],
		methodStorage:      4096,
		methodDescription:  2800,
		methodWebsite:      175,
		methodVideoComment: 4096,
		methodPhotoComment: 2048,
	}
	methodsMaxLenPayload = map[int]int{
		methodMessage:      datagramCalcMaxLen(methodsMaxLenEncoded[methodMessage] - datagramHeaderLenEncoded),
		methodPost:         datagramCalcMaxLen(methodsMaxLenEncoded[methodPost] - datagramHeaderLenEncoded),
		methodComment:      datagramCalcMaxLen(methodsMaxLenEncoded[methodComment] - datagramHeaderLenEncoded),
		methodDoc:          datagramCalcMaxLen(methodsMaxLenEncoded[methodDoc] - datagramHeaderLenEncoded),
		methodQR:           datagramCalcMaxLen(methodsMaxLenEncoded[methodQR] - datagramHeaderLenEncoded),
		methodStorage:      datagramCalcMaxLen(methodsMaxLenEncoded[methodStorage] - datagramHeaderLenEncoded),
		methodDescription:  datagramCalcMaxLen(methodsMaxLenEncoded[methodDescription] - datagramHeaderLenEncoded),
		methodWebsite:      datagramCalcMaxLen(methodsMaxLenEncoded[methodWebsite] - datagramHeaderLenEncoded),
		methodVideoComment: datagramCalcMaxLen(methodsMaxLenEncoded[methodVideoComment] - datagramHeaderLenEncoded),
		methodPhotoComment: datagramCalcMaxLen(methodsMaxLenEncoded[methodPhotoComment] - datagramHeaderLenEncoded),
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

func isSessionOpened() bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	for _, ses := range sessions {
		if !ses.isClosed() {
			return true
		}
	}

	return false
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
	openedAt  time.Time
	activity  time.Time
	posts     map[configClub]wallPostResponse
	inBytes   int
	outBytes  int
}

func openSession(id dgSes, cfg config) (*session, error) {
	slog.Debug("session: open", "id", id)

	now := time.Now()
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
		writes:    make(chan []byte, 500),
		datagrams: make(chan datagram, 500),
		openedAt:  now,
		activity:  now,
		posts:     make(map[configClub]wallPostResponse),
		inBytes:   0,
		outBytes:  0,
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

	slog.Debug(
		"session: stats",
		"id", s.id,
		"in", s.inBytes,
		"out", s.outBytes,
		"duration", int(time.Since(s.openedAt).Seconds()),
		"fragments", len(s.history),
	)

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

	timeout := s.cfg.Session.Timeout()

	if timeout == 0 {
		return false
	}

	return time.Since(s.activity) > timeout
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
	s.inBytes += len(b)

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
	s.outBytes += len(dg.payload)

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
	smallMethods := []int{methodMessage, methodPost}
	bigMethods := []int{methodDoc}

	if enabled := methodsEnabled[methodQR]; enabled {
		smallMethods = append(smallMethods, methodQR)
	}

	if enabled := methodsEnabled[methodVideoComment]; enabled {
		smallMethods = append(smallMethods, methodVideoComment)
	}

	if enabled := methodsEnabled[methodPhotoComment]; enabled {
		smallMethods = append(smallMethods, methodPhotoComment)
	}

	s.mu.Lock()

	if len(s.posts) > 0 {
		smallMethods = append(smallMethods, methodComment, methodComment)
	}

	s.mu.Unlock()

	if dg.command != commandConnect {
		smallMethods = append(smallMethods, methodStorage, methodStorage)
	}

	methods := []int{}
	fragments := []datagram{}

	maxSmallForwardLen := min(methodsMaxLenEncoded[methodQR], methodsMaxLenEncoded[methodPhotoComment])

	if dg.command != commandForward || dg.LenEncoded() <= maxSmallForwardLen {
		if dg.number == 0 {
			dg.number = s.nextNumber()
		}

		method := randElem(smallMethods)
		methods = append(methods, method)
		fragments = append(fragments, dg)

		return methods, fragments, nil
	}

	if dg.number != 0 {
		availableMethods := []int{}

		for _, m := range bigMethods {
			if dg.LenEncoded() <= methodsMaxLenEncoded[m] {
				availableMethods = append(availableMethods, m)
			}
		}

		if len(availableMethods) == 0 {
			return nil, nil, errors.New("no methods available")
		}

		method := randElem(availableMethods)
		methods = append(methods, method)
		fragments = append(fragments, dg)

		return methods, fragments, nil
	}

	for len(dg.payload) > 0 {
		method := randElem(bigMethods)
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
		case methodStorage:
			f = s.executeMethodStorage
		case methodDescription:
			f = s.executeMethodDescription
		case methodWebsite:
			f = s.executeMethodWebsite
		case methodVideoComment:
			f = s.executeMethodVideoComment
		case methodPhotoComment:
			f = s.executeMethodPhotoComment
		default:
			return fmt.Errorf("unknown method: %v", method)
		}

		encoded := encodeDatagram(fg, methodsEncoding[method])
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
			encoded[i] = encodeDatagram(fg, methodsEncoding[methodQR])
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
	club := randElem(s.cfg.Clubs)
	user := randElem(s.cfg.Users)
	p := messagesSendParams{
		message: encoded,
	}
	_, err := messagesSend(s.cfg.API, club, user, p)

	return err
}

func (s *session) executeMethodPost(encoded string) error {
	club := randElem(s.cfg.Clubs)
	p := wallPostParams{
		message: encoded,
	}
	resp, err := wallPost(s.cfg.API, club, p)

	if err != nil {
		return err
	}

	s.mu.Lock()
	s.posts[club] = resp
	s.mu.Unlock()

	return nil
}

func (s *session) executeMethodComment(encoded string) error {
	s.mu.Lock()

	if len(s.posts) == 0 {
		s.mu.Unlock()
		return errors.New("no posts created")
	}

	clubs := []configClub{}

	for key := range s.posts {
		clubs = append(clubs, key)
	}

	club := randElem(clubs)
	post := s.posts[club]

	s.mu.Unlock()

	p := wallCreateCommentParams{
		postID:  post.PostID,
		message: encoded,
	}
	_, err := wallCreateComment(s.cfg.API, club, p)

	return err
}

func (s *session) executeMethodDoc(encoded string) error {
	club := randElem(s.cfg.Clubs)
	uploadP := docsUploadParams{
		data: []byte(encoded),
	}
	resp, err := docsUploadAndSave(s.cfg.API, club, uploadP)

	if err != nil {
		return err
	}

	zero := encodeDatagram(newDatagram(0, 0, 0, nil), datagramEncodingASCII)
	arg := "caption=" + url.QueryEscape(zero)
	uri := resp.Doc.URL

	if strings.Contains(uri, "?") {
		uri += "&" + arg
	} else {
		uri += "?" + arg
	}

	msg := strings.ReplaceAll(uri, ".", ". ")
	methods := []int{methodMessage, methodPost, methodStorage, methodStorage, methodDescription, methodWebsite}

	if enabled := methodsEnabled[methodQR]; enabled {
		methods = append(methods, methodQR)
	}

	if enabled := methodsEnabled[methodVideoComment]; enabled {
		methods = append(methods, methodVideoComment)
	}

	if enabled := methodsEnabled[methodPhotoComment]; enabled {
		methods = append(methods, methodPhotoComment)
	}

	s.mu.Lock()

	if len(s.posts) > 0 {
		methods = append(methods, methodComment, methodComment)
	}

	s.mu.Unlock()

	method := randElem(methods)

	switch method {
	case methodMessage:
		err = s.executeMethodMessage(msg)
	case methodPost:
		err = s.executeMethodPost(msg)
	case methodComment:
		err = s.executeMethodComment(msg)
	case methodQR:
		err = s.executeMethodQR([]string{zero}, msg)
	case methodStorage:
		err = s.executeMethodStorage(msg)
	case methodDescription:
		err = s.executeMethodDescription(msg)
	case methodWebsite:
		err = s.executeMethodWebsite(msg)
	case methodVideoComment:
		err = s.executeMethodVideoComment(msg)
	case methodPhotoComment:
		err = s.executeMethodPhotoComment(msg)
	default:
		err = fmt.Errorf("unknown method: %v", method)
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
		zero := encodeDatagram(newDatagram(0, 0, 0, nil), datagramEncodingRU)
		caption = zero
	}

	club := randElem(s.cfg.Clubs)
	user := randElem(s.cfg.Users)
	p := photosUploadAndSaveParams{
		photosUploadParams: photosUploadParams{
			data: qr,
		},
		photosSaveParams: photosSaveParams{
			caption: caption,
		},
	}

	if _, err := photosUploadAndSave(s.cfg.API, club, user, p); err != nil {
		return fmt.Errorf("upload: %v", err)
	}

	return nil
}

func (s *session) executeMethodStorage(encoded string) error {
	club := randElem(s.cfg.Clubs)
	p := storageSetParams{
		key:    createStorageSetKey(),
		value:  encoded,
		userID: club.ID,
	}
	err := storageSet(s.cfg.API, club, p)

	return err
}

func (s *session) executeMethodDescription(encoded string) error {
	club := randElem(s.cfg.Clubs)
	p := groupsEditParams{
		description: encoded,
	}
	err := groupsEdit(s.cfg.API, club, p)

	return err
}

func (s *session) executeMethodWebsite(encoded string) error {
	club := randElem(s.cfg.Clubs)
	p := groupsEditParams{
		website: encoded,
	}
	err := groupsEdit(s.cfg.API, club, p)

	return err
}

func (s *session) executeMethodVideoComment(encoded string) error {
	club := randElem(s.cfg.Clubs)
	user := randElem(s.cfg.Users)
	p := videoCreateCommentParams{
		message: encoded,
	}
	err := videoCreateComment(s.cfg.API, club, user, p)

	return err
}

func (s *session) executeMethodPhotoComment(encoded string) error {
	club := randElem(s.cfg.Clubs)
	user := randElem(s.cfg.Users)
	p := photosCreateCommentParams{
		message: encoded,
	}
	err := photosCreateComment(s.cfg.API, club, user, p)

	return err
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

func randElem[T any](elems []T) T {
	if len(elems) == 0 {
		return *new(T)
	}

	n := rand.Intn(len(elems))

	return elems[n]
}

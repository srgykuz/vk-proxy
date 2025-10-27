package main

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

var (
	errSessionClosed    = errors.New("session is closed")
	errSessionQueueFull = errors.New("session queue is full")
)

var sessions map[dgSes]*session = map[dgSes]*session{}
var sessionsMu *sync.Mutex = &sync.Mutex{}

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
var sessionIDMu *sync.Mutex = &sync.Mutex{}

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
	datagrams chan string
	forwards  chan []byte
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
		datagrams: make(chan string, cfg.Session.QueueSize),
		forwards:  make(chan []byte, cfg.Session.QueueSize),
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

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.listenForwards()
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
	close(s.forwards)

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

	select {
	case s.writes <- clone:
		return nil
	default:
		return errSessionQueueFull
	}
}

func (s *session) listenWrites() {
	for data := range s.writes {
		if err := s.handleWrite(data); err != nil {
			slog.Error("session: write", "id", s.id, "err", err)
		}
	}
}

func (s *session) handleWrite(data []byte) error {
	return writeSocks(s.cfg, s, data)
}

func (s *session) sendDatagram(dg datagram) error {
	clone := dg.clone()
	encoded := encodeDatagram(dg)

	slog.Debug("session: send", "id", s.id, "dg", dg)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}

	s.history[clone.number] = clone

	select {
	case s.datagrams <- encoded:
		return nil
	default:
		return errSessionQueueFull
	}
}

func (s *session) listenDatagrams() {
	interval := s.cfg.API.Interval()

	for data := range s.datagrams {
		if err := s.handleSend(data); err != nil {
			slog.Error("session: send", "id", s.id, "err", err)
		}

		time.Sleep(interval)
	}
}

func (s *session) handleSend(data string) error {
	qr, err := encodeQR(s.cfg, data)

	if err != nil {
		return fmt.Errorf("encode: %v", err)
	}

	p := photosUploadParams{
		data: qr,
	}

	if _, err = photosUploadAndSave(s.cfg, p); err != nil {
		return fmt.Errorf("upload: %v", err)
	}

	return nil
}

func (s *session) sendForward(pld []byte) error {
	clone := bytes.Clone(pld)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}

	select {
	case s.forwards <- clone:
		return nil
	default:
		return errSessionQueueFull
	}
}

func (s *session) listenForwards() {
	interval := s.cfg.API.Interval()

	for data := range s.forwards {
		if err := s.handleForward(data); err != nil {
			slog.Error("session: forward", "id", s.id, "err", err)
		}

		time.Sleep(interval)
	}
}

func (s *session) handleForward(data []byte) error {
	chunks := bytesToChunks(data, s.cfg.QR.MergeSize)
	datagrams := make([]datagram, 0, len(chunks))

	for _, chunk := range chunks {
		num := s.nextNumber()
		dg := newDatagram(s.id, num, commandForward, chunk)
		datagrams = append(datagrams, dg)

		slog.Debug("session: forward", "id", s.id, "dg", dg)

		s.mu.Lock()
		s.history[dg.number] = dg
		s.mu.Unlock()
	}

	codes := make([][]byte, 0, len(datagrams))

	for _, dg := range datagrams {
		content := encodeDatagram(dg)
		qr, err := encodeQR(s.cfg, content)

		if err != nil {
			return fmt.Errorf("encode: %v", err)
		}

		codes = append(codes, qr)
	}

	qr, err := mergeQR(s.cfg, codes)

	if err != nil {
		return fmt.Errorf("merge: %v", err)
	}

	p := photosUploadParams{
		data: qr,
	}

	if _, err = photosUploadAndSave(s.cfg, p); err != nil {
		return fmt.Errorf("upload: %v", err)
	}

	return nil
}

func clearSession(cfg config) error {
	interval := cfg.Session.ClearInterval()

	for {
		time.Sleep(interval)

		sessionsMu.Lock()

		for id, ses := range sessions {
			if ses.isClosed() {
				delete(sessions, id)
			}
		}

		sessionsMu.Unlock()
	}
}

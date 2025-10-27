package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

var (
	errSessionClosed = errors.New("session is closed")
)

var sessions map[dgSes]*session = map[dgSes]*session{}
var sessionsMu sync.Mutex

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

func clearSessions(cfg config) error {
	interval := cfg.Session.ClearInterval()

	for {
		time.Sleep(interval)

		sessionsMu.Lock()

		for id, ses := range sessions {
			if ses.opened() {
				continue
			}

			delete(sessions, id)
		}

		sessionsMu.Unlock()
	}
}

var sessionID dgSes = 0
var sessionIDMu sync.Mutex

func nextSessionID() dgSes {
	sessionIDMu.Lock()
	defer sessionIDMu.Unlock()

	sessionID++

	return sessionID
}

type session struct {
	id       dgSes
	mu       sync.Mutex
	wg       sync.WaitGroup
	number   dgNum
	peer     net.Conn
	isClosed bool
	closed   chan struct{}
	writes   chan []byte
	messages chan string
	forwards chan []byte
	postID   int
	history  map[dgNum]datagram
}

func openSession(id dgSes, cfg config) (*session, error) {
	slog.Debug("session: open", "id", id)

	s := &session{
		id:       id,
		mu:       sync.Mutex{},
		wg:       sync.WaitGroup{},
		number:   0,
		peer:     nil,
		isClosed: false,
		closed:   make(chan struct{}),
		writes:   make(chan []byte, 500),
		messages: make(chan string, 500),
		forwards: make(chan []byte, 500),
		postID:   0,
		history:  make(map[dgNum]datagram),
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleWrites(cfg)
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleMessages(cfg)
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleForwards(cfg)
	}()

	return s, nil
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return
	}

	if s.peer == nil {
		slog.Debug("session: close", "id", s.id)
	} else {
		slog.Debug("session: close", "id", s.id, "peer", s.peer.RemoteAddr().String())
	}

	close(s.writes)
	close(s.messages)
	close(s.forwards)

	s.wg.Wait()

	if s.peer != nil {
		s.peer.Close()
	}

	close(s.closed)
	s.isClosed = true
}

func (s *session) opened() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return !s.isClosed
}

func (s *session) write(b []byte) error {
	clone := make([]byte, len(b))
	copy(clone, b)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return errSessionClosed
	}

	if s.peer == nil {
		return errors.New("write: peer is nil")
	}

	select {
	case s.writes <- clone:
		return nil
	default:
		return errors.New("write: queue is full")
	}
}

func (s *session) handleWrites(cfg config) {
	for b := range s.writes {
		if err := writeSocks(cfg, s, b); err != nil {
			slog.Error("session: handle writes", "id", s.id, "err", err)
		}
	}
}

func (s *session) message(dg datagram) error {
	msg := encodeDatagram(dg)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return errSessionClosed
	}

	slog.Debug("session: message", "id", s.id, "dg", dg)

	s.history[dg.number] = dg

	select {
	case s.messages <- msg:
		return nil
	default:
		return errors.New("message: queue is full")
	}
}

func (s *session) handleMessages(cfg config) {
	interval := cfg.API.Interval()

	for msg := range s.messages {
		qr, err := encodeQR(cfg, msg)

		if err == nil {
			p := photosUploadParams{
				data: qr,
			}
			_, err = photosUploadAndSave(cfg, p)
		} else {
			err = fmt.Errorf("encodeQR: %v", err)
		}

		if err != nil {
			slog.Error("session: handle messages", "id", s.id, "err", err)
		}

		time.Sleep(interval)
	}
}

func (s *session) forward(pld []byte) error {
	clone := make([]byte, len(pld))
	copy(clone, pld)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return errSessionClosed
	}

	select {
	case s.forwards <- clone:
		return nil
	default:
		return errors.New("forward: queue is full")
	}
}

func (s *session) handleForwards(cfg config) {
	interval := cfg.API.Interval()
	encode := func(pld []byte) ([]byte, error) {
		chunks := bytesToChunks(pld, cfg.QR.MergeSize)
		data := [][]byte{}

		for _, chunk := range chunks {
			num := s.nextNumber()
			dg := newDatagram(s.id, num, commandForward, chunk)

			s.mu.Lock()
			s.history[dg.number] = dg
			s.mu.Unlock()

			content := encodeDatagram(dg)

			slog.Debug("session: forward", "id", s.id, "dg", dg)

			qr, err := encodeQR(cfg, content)

			if err != nil {
				return nil, err
			}

			data = append(data, qr)
		}

		return mergeQR(cfg, data)
	}

	for pld := range s.forwards {
		qr, err := encode(pld)

		if err == nil {
			p := photosUploadParams{
				data: qr,
			}
			_, err = photosUploadAndSave(cfg, p)
		} else {
			err = fmt.Errorf("encode: %v", err)
		}

		if err != nil {
			slog.Error("session: handle forwards", "id", s.id, "err", err)
		}

		time.Sleep(interval)
	}
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

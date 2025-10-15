package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

const (
	signalConnected int = iota + 1
)

var (
	errSessionClosed = errors.New("session is closed")
)

var sessions map[int32]*session = map[int32]*session{}
var sessionsMu sync.Mutex

func getSession(id int32) (*session, bool) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	ses, exists := sessions[id]

	return ses, exists
}

func setSession(id int32, ses *session) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	sessions[id] = ses
}

var sessionID int32 = 0
var sessionIDMu sync.Mutex

func nextSessionID() int32 {
	sessionIDMu.Lock()
	defer sessionIDMu.Unlock()

	sessionID++

	return sessionID
}

type session struct {
	id        int32
	mu        sync.Mutex
	wg        sync.WaitGroup
	number    int32
	peer      net.Conn
	closed    bool
	writes    chan []byte
	messages  chan string
	sigConn   chan struct{}
	sigConnCl bool
}

func openSession(id int32, cfg config) (*session, error) {
	slog.Debug("session: open", "id", id)

	s := &session{
		id:        id,
		mu:        sync.Mutex{},
		wg:        sync.WaitGroup{},
		number:    0,
		peer:      nil,
		closed:    false,
		writes:    make(chan []byte, 100),
		messages:  make(chan string, 100),
		sigConn:   make(chan struct{}),
		sigConnCl: false,
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

	return s, nil
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	if s.peer == nil {
		slog.Debug("session: close", "id", s.id)
	} else {
		slog.Debug("session: close", "id", s.id, "peer", s.peer.RemoteAddr().String())
	}

	if !s.sigConnCl {
		close(s.sigConn)
		s.sigConnCl = true
	}

	close(s.writes)
	close(s.messages)

	s.wg.Wait()

	if s.peer != nil {
		s.peer.Close()
	}

	s.closed = true
}

func (s *session) opened() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return !s.closed
}

func (s *session) write(b []byte) error {
	clone := make([]byte, len(b))
	copy(clone, b)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
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

	if s.closed {
		return errSessionClosed
	}

	slog.Debug("session: message", "id", s.id, "dg", dg)

	select {
	case s.messages <- msg:
		return nil
	default:
		return errors.New("message: queue is full")
	}
}

func (s *session) handleMessages(cfg config) {
	for msg := range s.messages {
		p := messagesSendParams{
			message: msg,
		}

		if _, err := messagesSend(cfg, p); err != nil {
			slog.Error("session: handle messages", "id", s.id, "err", err)
		}
	}
}

func (s *session) signal(sig int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}

	slog.Debug("session: signal", "id", s.id, "sig", sig)

	switch sig {
	case signalConnected:
		if s.sigConnCl {
			return errors.New("signalConnected already done")
		} else {
			close(s.sigConn)
			s.sigConnCl = true
		}
	default:
		return fmt.Errorf("signal: unknown signal: %v", sig)
	}

	return nil
}

func (s *session) waitSignal(sig int) error {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return errSessionClosed
	}

	s.mu.Unlock()

	switch sig {
	case signalConnected:
		<-s.sigConn
	default:
		return fmt.Errorf("wait signal: unknown signal: %v", sig)
	}

	return nil
}

func (s *session) nextNumber() int32 {
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

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
	datagrams chan datagram
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
		datagrams: make(chan datagram, 100),
		sigConn:   make(chan struct{}),
		sigConnCl: false,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.listen(cfg)
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
		s.peer.Close()
	}

	if !s.sigConnCl {
		close(s.sigConn)
		s.sigConnCl = true
	}

	close(s.datagrams)
	s.wg.Wait()

	s.closed = true
}

func (s *session) listen(cfg config) {
	for dg := range s.datagrams {
		str := encodeDatagram(dg)
		p := messagesSendParams{
			message: str,
		}

		slog.Debug("session: send", "id", s.id, "dg", dg)

		if _, err := messagesSend(cfg, p); err != nil {
			slog.Error("session: send", "id", s.id, "dg", dg, "err", err)
		}
	}
}

func (s *session) send(dg datagram) error {
	clone := dg.clone()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}

	select {
	case s.datagrams <- clone:
		return nil
	default:
		return errors.New("send: queue is full")
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

func (s *session) wait(sig int) error {
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
		return fmt.Errorf("wait: unknown signal: %v", sig)
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

func (s *session) getPeer() net.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.peer
}

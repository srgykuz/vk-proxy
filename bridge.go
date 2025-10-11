package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

type link struct {
	brg  *bridge
	peer net.Conn
}

var links map[int32]link = map[int32]link{}
var linksMu sync.Mutex

func getLink(id int32) (link, bool) {
	linksMu.Lock()
	defer linksMu.Unlock()

	l, exists := links[id]

	return l, exists
}

func setLink(id int32, l link) {
	linksMu.Lock()
	defer linksMu.Unlock()

	links[id] = l
}

var counter int32 = 0
var counterMu sync.Mutex

func nextID() int32 {
	counterMu.Lock()
	defer counterMu.Unlock()

	counter++

	return counter
}

const (
	bridgeSignalConnected int = iota
)

var (
	errBridgeClosed = errors.New("bridge is closed")
)

type bridge struct {
	id        int32
	mu        sync.Mutex
	wg        sync.WaitGroup
	closed    bool
	datagrams chan datagram
	sigConn   chan struct{}
	sigConnCl bool
}

func openBridge(cfg config, id int32) (*bridge, error) {
	if id == 0 {
		id = nextID()
	}

	b := &bridge{
		id:        id,
		mu:        sync.Mutex{},
		wg:        sync.WaitGroup{},
		closed:    false,
		datagrams: make(chan datagram, 50),
		sigConn:   make(chan struct{}),
		sigConnCl: false,
	}

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.listen(cfg)
	}()

	slog.Debug("bridge: opened", "id", b.id)

	return b, nil
}

func (b *bridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true

	close(b.datagrams)

	if !b.sigConnCl {
		close(b.sigConn)
		b.sigConnCl = true
	}

	b.wg.Wait()

	slog.Debug("bridge: closed", "id", b.id)
}

func (b *bridge) listen(cfg config) {
	for dg := range b.datagrams {
		s := encodeDatagram(dg)
		p := messagesSendParams{
			message: s,
		}

		slog.Debug("bridge: sending", "sid", dg.session, "cmd", dg.command, "pld", len(dg.payload))

		if _, err := messagesSend(cfg, p); err != nil {
			slog.Error("bridge: sending failed", "err", err, "sid", dg.session, "cmd", dg.command, "pld", len(dg.payload))
		}
	}
}

func (b *bridge) send(dg datagram) error {
	clone := dg.clone()

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return errBridgeClosed
	}

	select {
	case b.datagrams <- clone:
		return nil
	default:
		return errors.New("queue is full")
	}
}

func (b *bridge) signal(sig int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return errBridgeClosed
	}

	switch sig {
	case bridgeSignalConnected:
		if b.sigConnCl {
			return errors.New("bridgeSignalConnected can be called only once")
		} else {
			close(b.sigConn)
			b.sigConnCl = true
		}
	default:
		return fmt.Errorf("unknown signal - %v", sig)
	}

	return nil
}

func (b *bridge) wait(sig int) error {
	b.mu.Lock()

	if b.closed {
		b.mu.Unlock()
		return errBridgeClosed
	}

	b.mu.Unlock()

	switch sig {
	case bridgeSignalConnected:
		<-b.sigConn
	default:
		return fmt.Errorf("unknown signal - %v", sig)
	}

	return nil
}

package main

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var bridges map[int32]*bridge = map[int32]*bridge{}
var bridgesMu sync.Mutex

func getBridge(id int32) (*bridge, bool) {
	bridgesMu.Lock()
	defer bridgesMu.Unlock()

	b, exists := bridges[id]

	return b, exists
}

func setBridge(b *bridge) {
	bridgesMu.Lock()
	defer bridgesMu.Unlock()

	bridges[b.id] = b
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

func openBridge(id int32) (*bridge, error) {
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
		b.listen()
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

func (b *bridge) listen() {
	for dg := range b.datagrams {
		s := encodeDatagram(dg)
		p := messagesSendParams{
			message: s,
		}

		slog.Debug("bridge: sending", "sid", dg.session, "cmd", dg.command, "pld", len(dg.payload))

		if _, err := messagesSend(p); err != nil {
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

var deviceID = time.Now().UnixMilli()

type datagram struct {
	version int16
	device  int64
	session int32
	command int16
	payload []byte
}

func (dg datagram) isLoopback() bool {
	return dg.device == deviceID
}

func (dg datagram) clone() datagram {
	pld := make([]byte, len(dg.payload))
	copy(pld, dg.payload)
	dg.payload = pld

	return dg
}

func encodeDatagram(dg datagram) string {
	data := []byte{}

	data = binary.BigEndian.AppendUint16(data, uint16(dg.version))
	data = binary.BigEndian.AppendUint64(data, uint64(dg.device))
	data = binary.BigEndian.AppendUint32(data, uint32(dg.session))
	data = binary.BigEndian.AppendUint16(data, uint16(dg.command))
	data = append(data, dg.payload...)

	s := base64.StdEncoding.EncodeToString(data)

	return s
}

func decodeDatagram(s string) (datagram, error) {
	data, err := base64.StdEncoding.DecodeString(s)

	if err != nil {
		return datagram{}, err
	}

	if len(data) < 16 {
		return datagram{}, errors.New("malformed datagram")
	}

	ver := int16(binary.BigEndian.Uint16(data[0:2]))
	dev := int64(binary.BigEndian.Uint64(data[2:10]))
	ses := int32(binary.BigEndian.Uint32(data[10:14]))
	cmd := int16(binary.BigEndian.Uint16(data[14:16]))
	pld := data[16:]

	dg := datagram{
		version: ver,
		device:  dev,
		session: ses,
		command: cmd,
		payload: pld,
	}

	return dg, nil
}

const (
	datagramCommandConnect int16 = iota + 1
	datagramCommandConnected
	datagramCommandForward
)

func newDatagram(ses int32, cmd int16, pld []byte) datagram {
	return datagram{
		version: 1,
		device:  deviceID,
		session: ses,
		command: cmd,
		payload: pld,
	}
}

type datagramPayloadConnect struct {
	host string
	port uint16
}

func (pld *datagramPayloadConnect) encode() []byte {
	data := []byte(pld.host)
	data = binary.BigEndian.AppendUint16(data, pld.port)

	return data
}

func (pld *datagramPayloadConnect) decode(data []byte) error {
	if len(data) < 2 {
		return errors.New("malformed payload")
	}

	hostb := data[:len(data)-2]
	pld.host = string(hostb)

	portb := data[len(data)-2:]
	pld.port = binary.BigEndian.Uint16(portb)

	return nil
}

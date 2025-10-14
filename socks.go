package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	stageHandshake int = iota
	stageConnect
	stageForward
)

var (
	errUnacceptable = errors.New("unacceptable")
	errUnsupported  = errors.New("unsupported")
	errPartialRead  = errors.New("partial read")
)

func listenSocks(cfg config) error {
	addr := address{cfg.Socks.ListenHost, cfg.Socks.ListenPort}.String()
	ln, err := net.Listen("tcp", addr)

	if err != nil {
		return err
	}

	slog.Info("socks: listening", "addr", addr)

	for {
		conn, err := ln.Accept()

		if err != nil {
			slog.Error("socks: accept", "err", err)
			continue
		}

		brg, err := openBridge(cfg, 0)

		if err != nil {
			slog.Error("socks: bridge", "err", err)
			conn.Close()
			continue
		}

		lk := link{
			brg:  brg,
			peer: conn,
		}
		setLink(brg.id, lk)

		go acceptSocks(cfg, conn, brg, stageHandshake)
	}
}

func acceptSocks(cfg config, conn net.Conn, brg *bridge, stage int) {
	defer brg.close()
	defer conn.Close()

	remote := conn.RemoteAddr().String()
	slog.Debug("socks: accepted", "remote", remote, "bridge", brg.id)

	err := handleSocks(cfg, conn, brg, stage)

	if err == nil {
		slog.Debug("socks: closed", "remote", remote, "bridge", brg.id)
	} else {
		slog.Error("socks: closed", "remote", remote, "bridge", brg.id, "err", err)
	}
}

type readBuffer struct {
	b    *bytes.Buffer
	mu   *sync.Mutex
	done chan struct{}
	errs []error
}

func handleSocks(cfg config, conn net.Conn, brg *bridge, stage int) error {
	var wg sync.WaitGroup
	var err error
	buf := readBuffer{
		b:    &bytes.Buffer{},
		mu:   &sync.Mutex{},
		done: make(chan struct{}),
		errs: []error{},
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(buf.done)
		err = readSocks(cfg, conn, buf, stage, brg)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardsSocks(cfg, buf, brg)
	}()

	wg.Wait()

	return err
}

func readSocks(cfg config, conn net.Conn, buf readBuffer, stage int, brg *bridge) error {
	remote := conn.RemoteAddr().String()
	temp := make([]byte, cfg.Socks.ReadSize)

	for {
		deadline := time.Now().Add(cfg.Socks.ConnectionDeadline())

		if err := conn.SetReadDeadline(deadline); err != nil {
			return err
		}

		readN, readErr := conn.Read(temp)

		if readN > 0 {
			in := temp[:readN]

			slog.Debug("socks: read", "remote", remote, "len", len(in))

			if cfg.Log.Payload {
				slog.Debug("socks: payload", "remote", remote, "in", bytesToHex(in))
			}

			var out []byte
			var err error

			switch stage {
			case stageHandshake:
				out, err = handleSocksStageHandshake(in)
				stage = stageConnect
			case stageConnect:
				var addr address
				addr, out, err = handleSocksStageConnect(in)

				if err == nil {
					err = handleSocksStageConnectBridge(brg, addr)
				}

				stage = stageForward
			case stageForward:
				buf.mu.Lock()

				if len(buf.errs) > 0 {
					err = buf.errs[0]
				} else {
					buf.b.Write(in)
				}

				buf.mu.Unlock()
			default:
				err = fmt.Errorf("unknown stage: %v", stage)
			}

			if len(out) > 0 {
				if writeErr := writeSocks(cfg, conn, out); writeErr != nil && err == nil {
					err = writeErr
				}
			}

			if err != nil {
				return err
			}
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}

		if readErr != nil {
			return readErr
		}
	}
}

func forwardsSocks(cfg config, buf readBuffer, brg *bridge) {
	interval := cfg.Socks.ForwardInterval()

	for {
		stop := false

		select {
		case <-buf.done:
			stop = true
		case <-time.After(interval):
		}

		var in []byte

		buf.mu.Lock()

		if buf.b.Len() > 0 {
			in = buf.b.Bytes()
			buf.b.Reset()
		}

		buf.mu.Unlock()

		if len(in) > 0 {
			slog.Debug("socks: forward", "bridge", brg.id, "len", len(in))

			err := handleSocksStageForward(in, brg, cfg.Socks.ChunkSize)

			if err != nil {
				buf.mu.Lock()
				buf.errs = append(buf.errs, err)
				buf.mu.Unlock()
			}
		}

		if stop {
			return
		}
	}
}

func writeSocks(cfg config, conn net.Conn, out []byte) error {
	remote := conn.RemoteAddr().String()

	slog.Debug("socks: write", "remote", remote, "len", len(out))

	if cfg.Log.Payload {
		slog.Debug("socks: payload", "remote", remote, "out", bytesToHex(out))
	}

	deadline := time.Now().Add(cfg.Socks.ConnectionDeadline())

	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}

	_, err := conn.Write(out)

	return err
}

func handleSocksStageHandshake(in []byte) ([]byte, error) {
	if in[0] != 0x05 {
		return nil, errUnacceptable
	}

	if len(in) < 2 {
		return nil, errPartialRead
	}

	nmethods := int(in[1])

	if len(in) < 2+nmethods {
		return nil, errPartialRead
	}

	methods := in[2 : 2+nmethods]

	if slices.Contains(methods, 0x00) {
		return []byte{0x05, 0x00}, nil
	}

	return []byte{0x05, 0xff}, errUnsupported
}

func handleSocksStageConnect(in []byte) (address, []byte, error) {
	if in[0] != 0x05 {
		return address{}, nil, errUnacceptable
	}

	if len(in) < 5 {
		return address{}, nil, errPartialRead
	}

	cmd := in[1]

	if cmd != 0x01 {
		return address{}, nil, errUnsupported
	}

	atyp := in[3]
	naddr := 0
	offset := 4

	switch atyp {
	case 0x01:
		naddr = 4
	case 0x03:
		naddr = int(in[4])
		offset = 5
	case 0x04:
		naddr = 16
	default:
		return address{}, nil, errUnsupported
	}

	if len(in) < offset+naddr+2 {
		return address{}, nil, errPartialRead
	}

	baddr := in[offset : offset+naddr]
	addr := ""

	if atyp == 0x03 {
		addr = string(baddr)
	} else {
		addr = net.IP(baddr).String()
	}

	port := binary.BigEndian.Uint16(in[offset+naddr : offset+naddr+2])
	dst := address{
		host: addr,
		port: port,
	}

	out := make([]byte, len(in))
	copy(out, in)
	out[1] = 0x00

	return dst, out, nil
}

func handleSocksStageConnectBridge(brg *bridge, addr address) error {
	num := brg.nextNumber()
	pld := payloadConnect(addr)
	b := pld.encode()
	dg := newDatagram(brg.id, num, commandConnect, b)

	if err := brg.send(dg); err != nil {
		return err
	}

	if err := brg.signal(bridgeSignalConnected); err != nil {
		return err
	}

	return nil
}

func handleSocksStageForward(in []byte, brg *bridge, chunkSize int) error {
	chunks := bytesToChunks(in, chunkSize)

	for _, chunk := range chunks {
		num := brg.nextNumber()
		dg := newDatagram(brg.id, num, commandForward, chunk)

		if err := brg.send(dg); err != nil {
			return err
		}
	}

	return nil
}

type address struct {
	host string
	port uint16
}

func (a address) String() string {
	if strings.Contains(a.host, ":") {
		return fmt.Sprintf("[%v]:%v", a.host, a.port)
	}

	return fmt.Sprintf("%v:%v", a.host, a.port)
}

func bytesToHex(b []byte) string {
	return fmt.Sprintf("% x", b)
}

func bytesToChunks(b []byte, size int) [][]byte {
	chunks := [][]byte{}

	for start := 0; start < len(b); start += size {
		end := min(start+size, len(b))
		chunk := b[start:end]
		chunks = append(chunks, chunk)
	}

	return chunks
}

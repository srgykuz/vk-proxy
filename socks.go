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
	stageHandshake int = iota + 1
	stageConnectV4
	stageConnectV5
	stageConnectSession
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

		ses, err := openSession(nextSessionID(), cfg)

		if err != nil {
			slog.Error("socks: session", "err", err)
			conn.Close()
			continue
		}

		ses.setPeer(conn)
		setSession(ses.id, ses)

		go acceptSocks(cfg, ses, stageHandshake)
	}
}

func acceptSocks(cfg config, ses *session, stage int) {
	peer := ses.peer.RemoteAddr().String()

	defer slog.Info("socks: closed", "peer", peer, "ses", ses)
	defer ses.close()

	slog.Debug("socks: accept", "peer", peer, "ses", ses)

	if err := handleSocks(cfg, ses, stage); err != nil {
		slog.Error("socks: handle", "peer", peer, "ses", ses, "err", err)
	}
}

type opBuffer struct {
	b    bytes.Buffer
	mu   sync.Mutex
	done chan struct{}
}

func handleSocks(cfg config, ses *session, stage int) error {
	var wg sync.WaitGroup
	var readErr error
	var fwdErr error
	fwdBuf := &opBuffer{
		b:    bytes.Buffer{},
		mu:   sync.Mutex{},
		done: make(chan struct{}),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(fwdBuf.done)

		readErr = readSocks(cfg, ses, stage, fwdBuf)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		fwdErr = forwardsSocks(cfg, ses, fwdBuf)

		if fwdErr != nil {
			ses.peer.Close()
		}
	}()

	wg.Wait()

	err := errors.Join(readErr, fwdErr)

	return err
}

func readSocks(cfg config, ses *session, stage int, fwdBuf *opBuffer) error {
	peer := ses.peer.RemoteAddr().String()
	temp := make([]byte, cfg.Socks.ReadSize)

	for {
		deadline := time.Now().Add(cfg.Socks.ReadTimeout())

		if err := ses.peer.SetReadDeadline(deadline); err != nil {
			return err
		}

		readN, readErr := ses.peer.Read(temp)

		if readN > 0 {
			in := temp[:readN]

			slog.Debug("socks: read", "peer", peer, "len", len(in))

			if cfg.Log.Payload {
				slog.Debug("socks: payload", "peer", peer, "in", bytesToHex(in))
			}

			if stage == stageHandshake && in[0] == 0x04 {
				stage = stageConnectV4
			}

			var out []byte
			var err error
			var addr address

			switch stage {
			case stageHandshake:
				out, err = handleStageHandshakeV5(in)
				stage = stageConnectV5
			case stageConnectV4:
				addr, out, err = handleStageConnectV4(in)

				if err == nil {
					stage = stageConnectSession
				}
			case stageConnectV5:
				addr, out, err = handleStageConnectV5(in)

				if err == nil {
					stage = stageConnectSession
				}
			}

			switch stage {
			case stageConnectSession:
				err = handleStageConnectSession(cfg, ses, addr)

				if err == nil {
					slog.Info("socks: forwarding", "peer", peer, "ses", ses, "addr", addr)
					stage = stageForward
				}
			case stageForward:
				fwdBuf.mu.Lock()
				fwdBuf.b.Write(in)
				fwdBuf.mu.Unlock()
			}

			if len(out) > 0 {
				if writeErr := writeSocks(cfg, ses, out); writeErr != nil && err == nil {
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

func forwardsSocks(cfg config, ses *session, buf *opBuffer) error {
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
			in = bytes.Clone(buf.b.Bytes())
			buf.b.Reset()
		}

		buf.mu.Unlock()

		if len(in) > 0 {
			slog.Debug("socks: forward", "ses", ses, "len", len(in))

			err := handleStageForward(ses, in, cfg.Socks.ForwardSize)

			if err != nil {
				return err
			}
		}

		if stop {
			return nil
		}
	}
}

func writeSocks(cfg config, ses *session, out []byte) error {
	peer := ses.peer.RemoteAddr().String()

	slog.Debug("socks: write", "peer", peer, "len", len(out))

	if cfg.Log.Payload {
		slog.Debug("socks: payload", "peer", peer, "out", bytesToHex(out))
	}

	deadline := time.Now().Add(cfg.Socks.WriteTimeout())

	if err := ses.peer.SetWriteDeadline(deadline); err != nil {
		return err
	}

	_, err := ses.peer.Write(out)

	return err
}

func handleStageHandshakeV5(in []byte) ([]byte, error) {
	if len(in) < 2 {
		return nil, errPartialRead
	}

	if in[0] != 0x05 {
		return nil, errUnacceptable
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

func handleStageConnectV4(in []byte) (address, []byte, error) {
	if len(in) < 9 {
		return address{}, nil, errPartialRead
	}

	vn := in[0]

	if vn != 0x04 {
		return address{}, nil, errUnacceptable
	}

	cd := in[1]

	if cd != 0x01 {
		return address{}, nil, errUnsupported
	}

	port := binary.BigEndian.Uint16(in[2:4])
	ip := in[4:8]
	chars := []rune{}

	if ip[0] == 0x00 && ip[1] == 0x00 && ip[2] == 0x00 && ip[3] != 0x00 {
		nulls := 0

		for _, b := range in[8:] {
			if b == 0x00 {
				nulls++
				continue
			}

			if nulls == 1 {
				chars = append(chars, rune(b))
			}
		}
	}

	addr := address{
		port: port,
	}

	if len(chars) > 0 {
		addr.host = string(chars)
	} else {
		addr.host = net.IP(ip).String()
	}

	out := bytes.Clone(in[:8])
	out[0] = 0x00
	out[1] = 0x5a

	return addr, out, nil
}

func handleStageConnectV5(in []byte) (address, []byte, error) {
	if len(in) < 5 {
		return address{}, nil, errPartialRead
	}

	ver := in[0]

	if ver != 0x05 {
		return address{}, nil, errUnacceptable
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

	out := bytes.Clone(in)
	out[1] = 0x00

	return dst, out, nil
}

func handleStageConnectSession(cfg config, ses *session, addr address) error {
	pld := payloadConnect(addr)
	encoded := pld.encode()
	encrypted, err := encrypt(encoded, cfg.Session.SecretKey)

	if err != nil {
		return err
	}

	dg := newDatagram(0, 0, commandConnect, encrypted)

	if err := ses.sendDatagram(dg); err != nil {
		return err
	}

	return nil
}

func handleStageForward(ses *session, in []byte, chunkSize int) error {
	chunks := bytesToChunks(in, chunkSize, 0)

	for _, chunk := range chunks {
		dg := newDatagram(0, 0, commandForward, chunk)

		if err := ses.sendDatagram(dg); err != nil {
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

func bytesToChunks(b []byte, size int, count int) [][]byte {
	if size == 0 || count == 1 {
		return [][]byte{b}
	}

	chunks := [][]byte{}

	for start := 0; start < len(b); start += size {
		end := min(start+size, len(b))
		chunk := b[start:end]
		chunks = append(chunks, chunk)
	}

	if count > 0 && len(chunks) > count-1 {
		counted := chunks[:count-1]
		remaining := chunks[count-1:]
		merged := []byte{}

		for _, b := range remaining {
			merged = append(merged, b...)
		}

		counted = append(counted, merged)

		return counted
	}

	return chunks
}

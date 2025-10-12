package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
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

		remote := conn.RemoteAddr().String()
		slog.Debug("socks: accepted", "remote", remote, "bridge", brg.id)

		lk := link{
			brg:  brg,
			peer: conn,
		}
		setLink(brg.id, lk)

		go func() {
			defer brg.close()
			defer conn.Close()

			err := handleSocks(cfg, conn, brg, stageHandshake)

			if err == nil {
				slog.Debug("socks: closed", "remote", remote, "bridge", brg.id)
			} else {
				slog.Error("socks: closed", "remote", remote, "bridge", brg.id, "err", err)
			}
		}()
	}
}

func handleSocks(cfg config, conn net.Conn, brg *bridge, stage int) error {
	remote := conn.RemoteAddr().String()
	buf := make([]byte, cfg.Socks.BufferSize)

	for {
		conn.SetDeadline(time.Now().Add(cfg.Socks.ConnectionDeadline()))

		n, readErr := conn.Read(buf)

		if n > 0 {
			in := buf[:n]

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
				slog.Info("socks: forwarding", "remote", remote, "bridge", brg.id, "bytes", len(in))
				err = handleSocksStageForward(in, brg)
			default:
				err = fmt.Errorf("unknown stage: %v", stage)
			}

			if len(out) > 0 {
				if cfg.Log.Payload {
					slog.Debug("socks: payload", "remote", remote, "out", bytesToHex(out))
				}

				if _, e := conn.Write(out); e != nil && err == nil {
					err = e
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
	pld := payloadConnect(addr)
	b := pld.encode()
	dg := newDatagram(brg.id, commandConnect, b)

	if err := brg.send(dg); err != nil {
		return err
	}

	if err := brg.wait(bridgeSignalConnected); err != nil {
		return err
	}

	return nil
}

func handleSocksStageForward(in []byte, brg *bridge) error {
	dg := newDatagram(brg.id, commandForward, in)
	err := brg.send(dg)

	return err
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

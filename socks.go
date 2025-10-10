package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"time"
)

var socksDeadline = time.Second * 30
var socksBufSize = 2048
var socksLogData = false

type address struct {
	host string
	port uint16
}

func (a address) String() string {
	return fmt.Sprintf("%v:%v", a.host, a.port)
}

func bytesToHex(b []byte) string {
	return fmt.Sprintf("% x", b)
}

func listenSocks(host string, port uint16) error {
	addr := address{host, port}.String()
	ln, err := net.Listen("tcp", addr)

	if err != nil {
		return err
	}

	slog.Info("socks5 server listening", "addr", addr)

	for {
		conn, err := ln.Accept()

		if err != nil {
			slog.Error("socks5 server", "err", err.Error())
			continue
		}

		brg, err := openBridge(0)

		if err != nil {
			slog.Error("socks5 server", "err", err.Error())
			conn.Close()
			continue
		}

		lk := link{
			brg:  brg,
			peer: conn,
		}
		setLink(brg.id, lk)

		remote := conn.RemoteAddr().String()
		slog.Debug("socks5 conn accepted", "remote", remote, "bridge", brg.id)

		go func() {
			defer brg.close()
			defer conn.Close()

			err := handleSocks(conn, brg, socksStageHandshake)

			if err == nil {
				slog.Debug("socks5 conn closed", "remote", remote, "bridge", brg.id)
			} else {
				slog.Error("socks5 conn closed", "remote", remote, "bridge", brg.id, "err", err.Error())
			}
		}()
	}
}

const (
	socksStageHandshake = iota
	socksStageConnect
	socksStageForward
)

var (
	errSocksUnacceptable = errors.New("data is malformed")
	errSocksUnsupported  = errors.New("logic is not supported")
	errSocksPartialRead  = errors.New("partial read is not supported")
)

func handleSocks(conn net.Conn, brg *bridge, stage int) error {
	remote := conn.RemoteAddr().String()
	buf := make([]byte, socksBufSize)

	for {
		conn.SetDeadline(time.Now().Add(socksDeadline))

		n, err := conn.Read(buf)

		if n > 0 {
			in := buf[:n]

			if socksLogData {
				slog.Debug("socks5 conn", "remote", remote, "in", bytesToHex(in))
			}

			var out []byte
			var err error

			switch stage {
			case socksStageHandshake:
				out, err = handleSocksStageHandshake(in)
				stage = socksStageConnect
			case socksStageConnect:
				var addr address
				addr, out, err = handleSocksStageConnect(in)

				if err == nil {
					err = handleSocksStageConnectBridge(brg, addr)
				}

				stage = socksStageForward
			case socksStageForward:
				slog.Info("socks5 conn", "remote", remote, "bridge", brg.id, "bytes", len(in))
				err = handleSocksStageForward(brg, in)
			default:
				return fmt.Errorf("unknown stage - %v", stage)
			}

			if len(out) > 0 {
				conn.Write(out)

				if socksLogData {
					slog.Debug("socks5 conn", "remote", remote, "out", bytesToHex(out))
				}
			}

			if err != nil {
				return err
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}
	}
}

func handleSocksStageHandshake(in []byte) ([]byte, error) {
	if in[0] != 0x05 {
		return nil, errSocksUnacceptable
	}

	if len(in) < 2 {
		return nil, errSocksPartialRead
	}

	nmethods := int(in[1])

	if len(in) < 2+nmethods {
		return nil, errSocksPartialRead
	}

	methods := in[2 : 2+nmethods]

	if slices.Contains(methods, 0x00) {
		return []byte{0x05, 0x00}, nil
	}

	return []byte{0x05, 0xff}, errSocksUnsupported
}

func handleSocksStageConnect(in []byte) (address, []byte, error) {
	if in[0] != 0x05 {
		return address{}, nil, errSocksUnacceptable
	}

	if len(in) < 5 {
		return address{}, nil, errSocksPartialRead
	}

	cmd := in[1]

	if cmd != 0x01 {
		return address{}, nil, errSocksUnsupported
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
		return address{}, nil, errSocksUnsupported
	}

	if len(in) < offset+naddr+2 {
		return address{}, nil, errSocksPartialRead
	}

	baddr := in[offset : offset+naddr]
	addr := ""

	if atyp == 0x03 {
		addr = string(baddr)
	} else {
		addr = net.IP(baddr).String()
	}

	port := uint16(binary.BigEndian.Uint16((in[offset+naddr : offset+naddr+2])))
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
	pld := datagramPayloadConnect(addr)
	pldb := pld.encode()
	dg := newDatagram(brg.id, datagramCommandConnect, pldb)

	if err := brg.send(dg); err != nil {
		return err
	}

	if err := brg.wait(bridgeSignalConnected); err != nil {
		return err
	}

	return nil
}

func handleSocksStageForward(brg *bridge, data []byte) error {
	dg := newDatagram(brg.id, datagramCommandForward, data)
	err := brg.send(dg)

	return err
}

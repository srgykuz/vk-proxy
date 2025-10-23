package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"time"
)

func listenLongPoll(cfg config) error {
	server, err := groupsGetLongPollServer(cfg)

	if err != nil {
		return err
	}

	last := groupsUseLongPollServerResponse{
		TS: server.TS,
	}

	slog.Info("long poll: listening")

	for {
		last, err = groupsUseLongPollServer(cfg, server, last)

		if err != nil {
			slog.Error("long poll: listen", "err", err)
			continue
		}

		if last.Failed != 0 {
			slog.Debug("long poll: refresh")

			server, err = groupsGetLongPollServer(cfg)

			if err == nil {
				last = groupsUseLongPollServerResponse{
					TS: server.TS,
				}
			} else {
				slog.Error("long poll: refresh", "err", err)
			}

			continue
		}

		for _, upd := range last.Updates {
			if err := handleUpdate(cfg, upd); err != nil {
				slog.Error("long poll: handle", "type", upd.Type, "err", err)
			}
		}
	}
}

func handleUpdate(cfg config, upd update) error {
	var encodedS string
	var encodedB []byte
	var datagrams []datagram
	var err error

	switch upd.TypeEnum() {
	case updateTypeMessageReply:
		encodedS = upd.Object.Text
	case updateTypeWallPostNew:
		encodedS = upd.Object.Text
	case updateTypeWallReplyNew:
		encodedS = upd.Object.Text
	case updateTypePhotoNew:
		datagrams, err = handleUpdatePhoto(cfg, upd.Object.OrigPhoto.URL)
	case updateTypeGroupChangeSettings:
		encodedB, err = apiDownloadURL(cfg, upd.Object.Changes.Website.NewValue)
	default:
		err = errors.New("unsupported update")
	}

	if err != nil {
		return err
	}

	if len(encodedB) > 0 {
		encodedS = string(encodedB)
	}

	if len(encodedS) > 0 {
		dg, err := handleEncoded(encodedS)

		if err != nil {
			return err
		}

		if !dg.isZero() {
			datagrams = append(datagrams, dg)
		}
	}

	for _, dg := range datagrams {
		slog.Debug("handler: update", "type", upd.Type, "dg", dg)

		if err := handleDatagram(cfg, dg); err != nil {
			slog.Error("handler: update", "type", upd.Type, "dg", dg, "err", err)
		}
	}

	return nil
}

func handleUpdatePhoto(cfg config, url string) ([]datagram, error) {
	b, err := apiDownloadURL(cfg, url)

	if err != nil {
		return nil, fmt.Errorf("download url: %v", err)
	}

	file, err := saveQR(cfg, b, "jpg")

	if err != nil {
		return nil, fmt.Errorf("save qr: %v", err)
	}

	defer os.Remove(file)

	content, err := decodeQR(cfg, file)

	if err != nil {
		return nil, fmt.Errorf("decode qr: %v", err)
	}

	datagrams := []datagram{}

	for _, s := range content {
		dg, err := handleEncoded(s)

		if err != nil {
			return nil, err
		}

		if !dg.isZero() {
			datagrams = append(datagrams, dg)
		}
	}

	sort.Slice(datagrams, func(i, j int) bool {
		return datagrams[i].number < datagrams[j].number
	})

	return datagrams, nil
}

func handleEncoded(s string) (datagram, error) {
	dg, err := decodeDatagram(s)

	if err != nil {
		return datagram{}, fmt.Errorf("decode datagram: %v", err)
	}

	if dg.isLoopback() {
		return datagram{}, nil
	}

	return dg, nil
}

func handleDatagram(cfg config, dg datagram) error {
	ses, exists := getSession(dg.session)

	if exists && dg.command == commandConnect {
		if ses.opened() {
			return errors.New("bidirectional proxying over opened session")
		}

		exists = false
	}

	var err error

	if !exists {
		ses, err = openSession(dg.session, cfg)

		if err != nil {
			return fmt.Errorf("open session: %v", err)
		}

		setSession(ses.id, ses)
	}

	if cfg.Log.Payload {
		slog.Debug("handler: payload", "ses", ses.id, "in", bytesToHex(dg.payload))
	}

	switch dg.command {
	case commandConnect:
		err = handleCommandConnect(cfg, ses, dg)

		if err == nil {
			slog.Info("handler: forwarding", "ses", ses.id)
		}
	case commandForward:
		err = handleCommandForward(ses, dg)
	case commandClose:
		handleCommandClose(ses, false)
	default:
		err = errors.New("unsupported")
	}

	if err != nil {
		if dg.command != commandClose {
			handleCommandClose(ses, true)
		}

		return fmt.Errorf("command %v: %v", dg.command, err)
	}

	return nil
}

func handleCommandConnect(cfg config, ses *session, dg datagram) error {
	pld := payloadConnect{}

	if err := pld.decode(dg.payload); err != nil {
		return err
	}

	addr := address(pld).String()
	timeout := 10 * time.Second
	conn, err := net.DialTimeout("tcp", addr, timeout)

	if err != nil {
		return err
	}

	ses.setPeer(conn)

	go acceptSocks(cfg, ses, stageForward)

	if err := ses.signal(signalConnected); err != nil {
		return err
	}

	return nil
}

func handleCommandForward(ses *session, dg datagram) error {
	if err := ses.waitSignal(signalConnected); err != nil {
		return err
	}

	if err := ses.write(dg.payload); err != nil {
		return err
	}

	return nil
}

func handleCommandClose(ses *session, notify bool) {
	if notify {
		num := ses.nextNumber()
		dg := newDatagram(ses.id, num, commandClose, nil)

		if err := ses.message(dg); err != nil {
			slog.Error("handler: command close: notify", "ses", ses.id, "err", err)
		}
	}

	ses.close()
}

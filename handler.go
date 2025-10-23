package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
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
	var encodedD []datagram
	var err error

	if len(upd.Object.Text) > 0 {
		encodedS = upd.Object.Text
	} else if len(upd.Object.Changes.Website.NewValue) > 0 {
		p := apiDownloadParams{
			url: upd.Object.Changes.Website.NewValue,
		}
		encodedB, err = apiDownload(cfg, p)
	} else if len(upd.Object.OrigPhoto.URL) > 0 {
		encodedD, err = handlePhoto(cfg, upd.Object.OrigPhoto.URL)
	} else {
		err = fmt.Errorf("unsupported update: %v", upd.Type)
	}

	if err != nil {
		return err
	}

	if len(encodedB) > 0 {
		encodedS = string(encodedB)
	}

	if len(encodedS) > 0 {
		dg, err := handleEncodedDatagram(encodedS)

		if err != nil {
			return err
		}

		if !dg.isZero() {
			encodedD = append(encodedD, dg)
		}
	}

	for _, dg := range encodedD {
		slog.Debug("wall: update", "type", upd.Type, "dg", dg)

		if cfg.Log.Payload {
			slog.Debug("wall: update", "type", upd.Type, "encoded", encodedS, "payload", bytesToHex(dg.payload))
		}

		if err := handleDatagram(cfg, dg); err != nil {
			return err
		}
	}

	return nil
}

func handlePhoto(cfg config, url string) ([]datagram, error) {
	p := apiDownloadParams{
		url: url,
	}
	b, err := apiDownload(cfg, p)

	if err != nil {
		return nil, fmt.Errorf("apiDownload: %v", err)
	}

	file, err := saveQR(cfg, b, "jpg")

	if err != nil {
		return nil, fmt.Errorf("saveQR: %v", err)
	}

	defer os.Remove(file)

	content, err := decodeQR(cfg, file)

	if err != nil {
		return nil, fmt.Errorf("decodeQR: %v", err)
	}

	dgs := []datagram{}

	for _, s := range content {
		dg, err := handleEncodedDatagram(s)

		if err != nil {
			return nil, fmt.Errorf("handleEncodedDatagram: %v", err)
		}

		if !dg.isZero() {
			dgs = append(dgs, dg)
		}
	}

	sort.Slice(dgs, func(i, j int) bool {
		return dgs[i].number < dgs[j].number
	})

	return dgs, nil
}

func handleEncodedDatagram(encoded string) (datagram, error) {
	dg, err := decodeDatagram(encoded)

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
			return fmt.Errorf("bidirectional proxying over opened session: %v", dg)
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

	slog.Debug("handler: handle", "ses", ses.id, "dg", dg)

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
		err = errors.New("unknown")
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
	conn, err := net.Dial("tcp", addr)

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

	err := ses.write(dg.payload)

	return err
}

func handleCommandClose(ses *session, notify bool) {
	if notify {
		num := ses.nextNumber()
		dg := newDatagram(ses.id, num, commandClose, nil)
		ses.message(dg)
	}

	ses.close()
}

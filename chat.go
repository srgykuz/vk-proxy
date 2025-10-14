package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"
)

func listenChat(cfg config) error {
	last, err := messagesGetLatest(cfg)

	if err != nil {
		return err
	}

	slog.Info("chat: listening")

	for {
		time.Sleep(cfg.Chat.CheckInterval())

		p := messagesGetHistoryParams{
			offset: last.ID + cfg.Chat.FetchOffset,
			count:  cfg.Chat.FetchCount,
			rev:    1,
		}
		resp, err := messagesGetHistory(cfg, p)

		if err != nil {
			slog.Error("chat: get new messages", "err", err)
			continue
		}

		if len(resp.Items) == 0 {
			continue
		}

		last = resp.Items[len(resp.Items)-1]

		for _, msg := range resp.Items {
			if err := handleMessage(cfg, msg); err != nil {
				slog.Error("chat: handle", "msg", msg.ID, "err", err)
			}
		}
	}
}

func handleMessage(cfg config, msg message) error {
	dg, err := decodeDatagram(msg.Text)

	if err != nil {
		return fmt.Errorf("decode datagram: %v", err)
	}

	if dg.isLoopback() {
		return nil
	}

	var ses *session
	var exists bool

	ses, exists = getSession(dg.session)

	if !exists {
		ses, err = openSession(dg.session, cfg)

		if err != nil {
			return fmt.Errorf("open session: %v", err)
		}

		setSession(ses.id, ses)
	}

	slog.Debug("chat: read", "msg", msg.ID, "ses", ses.id, "dg", dg)

	if cfg.Log.Payload {
		slog.Debug("chat: message", "id", msg.ID, "text", msg.Text, "payload", bytesToHex(dg.payload))
	}

	switch dg.command {
	case commandConnect:
		err = handleCommandConnect(cfg, ses, dg)
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

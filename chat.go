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

	slog.Debug("chat: handle", "msg", msg.ID, "ses", dg.session, "cmd", dg.command, "pld", len(dg.payload))

	if cfg.Log.Payload {
		slog.Debug("chat: message", "id", msg.ID, "text", msg.Text, "payload", bytesToHex(dg.payload))
	}

	var lk link
	var exists bool

	lk, exists = getLink(dg.session)

	if !exists {
		brg, err := openBridge(cfg, dg.session)

		if err != nil {
			return fmt.Errorf("open bridge: %v", err)
		}

		lk = link{
			brg: brg,
		}
		setLink(brg.id, lk)
	}

	switch dg.command {
	case commandConnect:
		err = handleCommandConnect(cfg, lk, dg)
	case commandForward:
		err = handleCommandForward(cfg, lk, dg)
	default:
		return fmt.Errorf("unknown command: %v", dg.command)
	}

	if err != nil {
		return fmt.Errorf("command %v: %v", dg.command, err)
	}

	return nil
}

func handleCommandConnect(cfg config, lk link, dg datagram) error {
	if lk.brg == nil {
		return errors.New("link is misconfigured")
	}

	pld := payloadConnect{}

	if err := pld.decode(dg.payload); err != nil {
		return err
	}

	addr := address(pld).String()
	conn, err := net.Dial("tcp", addr)

	if err != nil {
		return err
	}

	lk.peer = conn
	setLink(lk.brg.id, lk)

	go acceptSocks(cfg, lk.peer, lk.brg, stageForward)

	if err := lk.brg.signal(bridgeSignalConnected); err != nil {
		return err
	}

	return nil
}

func handleCommandForward(cfg config, lk link, dg datagram) error {
	if lk.brg == nil || lk.peer == nil {
		return errors.New("link is misconfigured")
	}

	if err := lk.brg.wait(bridgeSignalConnected); err != nil {
		return err
	}

	err := writeSocks(cfg, lk.peer, dg.payload)

	return err
}

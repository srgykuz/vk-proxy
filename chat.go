package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"
)

var chatCheckInterval = time.Second * 1
var chatFetchCount = 5
var chatFetchOffset = -1
var chatLogData = true

func listenChat() error {
	last, err := messagesGetLatest()

	if err != nil {
		return err
	}

	slog.Info("chat: listening", "last", last.Text)

	for {
		time.Sleep(chatCheckInterval)

		p := messagesGetHistoryParams{
			offset: last.ID + chatFetchOffset,
			count:  chatFetchCount,
			rev:    1,
		}
		resp, err := messagesGetHistory(p)

		if err != nil {
			slog.Error("chat: unable to get new messages", "err", err)
			continue
		}

		if len(resp.Items) == 0 {
			continue
		}

		last = resp.Items[len(resp.Items)-1]

		for _, msg := range resp.Items {
			if err := handleMessage(msg); err != nil {
				slog.Error("chat: can't handle message", "err", err, "text", msg.Text)
			}
		}
	}
}

func handleMessage(msg message) error {
	dg, err := decodeDatagram(msg.Text)

	if err != nil {
		return err
	}

	if dg.isLoopback() {
		return nil
	}

	if chatLogData {
		slog.Debug("chat: message", "id", msg.ID, "text", msg.Text)
	}

	if err := handleDatagram(dg); err != nil {
		slog.Error("chat: can't handle datagram", "err", err, "message id", msg.ID)
	}

	return nil
}

func handleDatagram(dg datagram) error {
	var lk link
	var exists bool

	lk, exists = getLink(dg.session)

	if !exists {
		brg, err := openBridge(dg.session)

		if err != nil {
			return err
		}

		lk = link{
			brg: brg,
		}
		setLink(brg.id, lk)
	}

	switch dg.command {
	case datagramCommandConnect:
		return handleDatagramCommandConnect(lk, dg)
	case datagramCommandConnected:
		return handleDatagramCommandConnected(lk)
	case datagramCommandForward:
		return handleDatagramCommandForward(lk, dg)
	default:
		return fmt.Errorf("unknown command - %v", dg.command)
	}
}

func handleDatagramCommandConnect(lk link, dg datagram) error {
	if lk.brg == nil {
		return errors.New("invalid link")
	}

	pld := datagramPayloadConnect{}

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

	go func() {
		defer lk.brg.close()
		defer lk.peer.Close()

		remote := lk.peer.RemoteAddr().String()
		err := handleSocks(lk.peer, lk.brg, socksStageForward)

		if err == nil {
			slog.Debug("socks5 conn closed", "remote", remote, "bridge", lk.brg.id)
		} else {
			slog.Error("socks5 conn closed", "remote", remote, "bridge", lk.brg.id, "err", err.Error())
		}
	}()

	back := newDatagram(lk.brg.id, datagramCommandConnected, nil)

	if err := lk.brg.send(back); err != nil {
		return err
	}

	slog.Debug("chat: connected", "bridge", lk.brg.id, "addr", addr)

	return nil
}

func handleDatagramCommandConnected(lk link) error {
	if lk.brg == nil {
		return errors.New("invalid link")
	}

	if err := lk.brg.signal(bridgeSignalConnected); err != nil {
		return err
	}

	slog.Debug("chat: confirm connection", "bridge", lk.brg.id)

	return nil
}

func handleDatagramCommandForward(lk link, dg datagram) error {
	if lk.brg == nil || lk.peer == nil {
		return errors.New("invalid link")
	}

	slog.Debug("chat: forwarding", "bridge", lk.brg.id, "pld", len(dg.payload))

	if err := lk.peer.SetWriteDeadline(time.Now().Add(socksDeadline)); err != nil {
		return err
	}

	if _, err := lk.peer.Write(dg.payload); err != nil {
		return err
	}

	return nil
}

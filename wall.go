package main

import (
	"log/slog"
)

func listenWall(cfg config) error {
	server, err := groupsGetLongPollServer(cfg)

	if err != nil {
		return err
	}

	last := groupsUseLongPollServerResponse{
		TS: server.TS,
	}

	slog.Info("wall: listening")

	for {
		last, err = groupsUseLongPollServer(cfg, server, last)

		if err != nil {
			slog.Error("wall: long poll", "err", err)
			continue
		}

		if last.Failed != 0 {
			slog.Debug("wall: long poll refresh")

			server, err = groupsGetLongPollServer(cfg)

			if err == nil {
				last = groupsUseLongPollServerResponse{
					TS: server.TS,
				}
			} else {
				slog.Error("wall: long poll refresh", "err", err)
			}

			continue
		}

		for _, upd := range last.Updates {
			if err := handleUpdate(cfg, upd); err != nil {
				slog.Error("wall: handle", "obj", upd.Object.ID, "err", err)
			}
		}
	}
}

func handleUpdate(cfg config, upd update) error {
	dg, err := handleEncodedDatagram(upd.Object.Text)

	if err != nil {
		return err
	}

	if dg.isZero() {
		return nil
	}

	slog.Debug("wall: read", "id", upd.Object.ID, "dg", dg)

	if cfg.Log.Payload {
		slog.Debug("wall: update", "id", upd.Object.ID, "text", upd.Object.Text, "payload", bytesToHex(dg.payload))
	}

	return handleDatagram(cfg, dg)
}

package main

import (
	"fmt"
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
				slog.Error("wall: handle", "type", upd.Type, "err", err)
			}
		}
	}
}

func handleUpdate(cfg config, upd update) error {
	var encoded string
	var err error

	if len(upd.Object.Text) > 0 {
		encoded = upd.Object.Text
	} else if len(upd.Object.Changes.Website.NewValue) > 0 {
		p := docsDownloadParams{
			url: upd.Object.Changes.Website.NewValue,
		}
		encoded, err = docsDownload(cfg, p)
	} else {
		err = fmt.Errorf("unsupported update: %v", upd.Type)
	}

	if err != nil {
		return err
	}

	dg, err := handleEncodedDatagram(encoded)

	if err != nil {
		return err
	}

	if dg.isZero() {
		return nil
	}

	slog.Debug("wall: update", "type", upd.Type, "dg", dg)

	if cfg.Log.Payload {
		slog.Debug("wall: update", "type", upd.Type, "encoded", encoded, "payload", bytesToHex(dg.payload))
	}

	return handleDatagram(cfg, dg)
}

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
			if err := handleUpdate(upd); err != nil {
				slog.Error("wall: handle", "upd", upd.EventID, "err", err)
			}
		}
	}
}

func handleUpdate(upd update) error {
	fmt.Printf("%+v\n", upd)
	return nil
}

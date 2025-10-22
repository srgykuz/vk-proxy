package main

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
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

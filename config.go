package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

type config struct {
	Log     configLog     `json:"log"`
	Session configSession `json:"session"`
	Socks   configSocks   `json:"socks"`
	API     configAPI     `json:"api"`
	QR      configQR      `json:"qr"`
	Clubs   []configClub  `json:"clubs"`
	Users   []configUser  `json:"users"`
}

type configLog struct {
	Level   int    `json:"level"`
	Output  string `json:"output"`
	Payload bool   `json:"payload"`
}

type configSession struct {
	TimeoutMS int    `json:"timeout"`
	Secret    string `json:"secret"`
	SecretKey []byte `json:"-"`
}

func (cfg configSession) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type configSocks struct {
	ListenHost        string `json:"listenHost"`
	ListenPort        uint16 `json:"listenPort"`
	ForwardSize       int    `json:"forwardSize"`
	ForwardIntervalMS int    `json:"forwardInterval"`
}

func (cfg configSocks) ForwardInterval() time.Duration {
	return time.Duration(cfg.ForwardIntervalMS) * time.Millisecond
}

type configAPI struct {
	TimeoutMS   int  `json:"-"`
	Unathorized bool `json:"unathorized"`
}

func (cfg configAPI) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type configQR struct {
	Disabled   bool   `json:"-"`
	ZBarPath   string `json:"zbarPath"`
	ImageSize  int    `json:"-"`
	ImageLevel int    `json:"-"`
	SaveDir    string `json:"saveDir"`
}

type configClub struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	AccessToken string `json:"accessToken"`
	AlbumID     string `json:"albumID"`
	PhotoID     string `json:"photoID"`
	VideoID     string `json:"videoID"`
}

type configUser struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	AccessToken string `json:"accessToken"`
}

func defaultConfig() config {
	return config{
		Log: configLog{
			Level: 0,
		},
		Session: configSession{
			TimeoutMS: 30 * 1000,
		},
		Socks: configSocks{
			ListenHost:        "127.0.0.1",
			ListenPort:        1080,
			ForwardSize:       1 * 1024 * 1024,
			ForwardIntervalMS: 500,
		},
		API: configAPI{
			TimeoutMS: 10 * 1000,
		},
		QR: configQR{
			Disabled:   false,
			ZBarPath:   "zbarimg",
			ImageSize:  512,
			ImageLevel: 1,
		},
	}
}

func parseConfig(name string) (config, error) {
	data, err := os.ReadFile(name)

	if err != nil {
		return config{}, err
	}

	cfg := defaultConfig()

	if len(data) == 0 {
		return cfg, nil
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, err
	}

	if len(cfg.Session.Secret) > 0 {
		key, err := secretToKey(cfg.Session.Secret)

		if err != nil {
			return config{}, err
		}

		cfg.Session.SecretKey = key
	}

	cfg.QR.Disabled = cfg.API.Unathorized || len(cfg.QR.ZBarPath) == 0

	return cfg, nil
}

func validateConfig(cfg config) error {
	if len(cfg.Clubs) == 0 {
		return errors.New("clubs are missing")
	}

	if len(cfg.Users) == 0 {
		return errors.New("users are missing")
	}

	for _, club := range cfg.Clubs {
		if club.Name == "" {
			return errors.New("club.name is missing")
		}

		if club.ID == "" {
			return errors.New("club.id is missing")
		}

		if club.AccessToken == "" {
			return errors.New("club.accessToken is missing")
		}

		if club.AlbumID == "" {
			return errors.New("club.albumID is missing")
		}

		if club.PhotoID == "" {
			return errors.New("club.photoID is missing")
		}

		if club.VideoID == "" {
			return errors.New("club.videoID is missing")
		}
	}

	for _, user := range cfg.Users {
		if user.Name == "" {
			return errors.New("user.name is missing")
		}

		if user.ID == "" {
			return errors.New("user.id is missing")
		}

		if user.AccessToken == "" && !cfg.API.Unathorized {
			return errors.New("user.accessToken is missing")
		}
	}

	if len(cfg.Session.SecretKey) == 0 {
		return errors.New("session.secret is missing")
	}

	return nil
}

func validateQR(cfg configQR) error {
	if cfg.Disabled {
		return nil
	}

	content := "test"
	data, err := encodeQR(cfg, content)

	if err != nil {
		return err
	}

	file, err := saveQR(cfg, data, "png")

	if err != nil {
		return err
	}

	defer os.Remove(file)

	decoded, err := decodeQR(cfg, file)

	if err != nil {
		return err
	}

	if len(decoded) != 1 {
		return errors.New("unexpected decoded data size")
	}

	if content != decoded[0] {
		return errors.New("encoded and decoded content mismatch")
	}

	return nil
}

func validateLongPoll(cfg configAPI, club configClub) error {
	settings, err := groupsGetLongPollSettings(cfg, club)

	if err != nil {
		return err
	}

	if !settings.IsEnabled {
		return errors.New("disabled")
	}

	required := []string{
		"message_reply",
		"photo_new",
		"photo_comment_new",
		"video_comment_new",
		"wall_post_new",
		"wall_reply_new",
		"group_change_settings",
	}

	for _, event := range required {
		enabled, exists := settings.Events[event]

		if !exists || enabled == 0 {
			return fmt.Errorf("%v disabled", event)
		}
	}

	return nil
}

func configureLogger(cfg configLog) error {
	if len(cfg.Output) == 0 {
		slog.SetLogLoggerLevel(slog.Level(cfg.Level))
		return nil
	}

	f, err := os.OpenFile(cfg.Output, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)

	if err != nil {
		return err
	}

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.Level(cfg.Level),
	})
	logger := slog.New(handler)

	slog.SetDefault(logger)

	return nil
}

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
	Clubs   []configClub  `json:"clubs"`
	Users   []configUser  `json:"users"`
	QR      configQR      `json:"qr"`
}

type configLog struct {
	Level   int    `json:"level"`
	Output  string `json:"output"`
	Payload bool   `json:"payload"`
}

type configSession struct {
	QueueSize int    `json:"queueSize"`
	TimeoutMS int    `json:"timeout"`
	Secret    string `json:"secret"`
	SecretKey []byte
}

func (cfg configSession) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type configSocks struct {
	ListenHost        string `json:"listenHost"`
	ListenPort        uint16 `json:"listenPort"`
	ReadSize          int    `json:"readSize"`
	ReadTimeoutMS     int    `json:"readTimeout"`
	WriteTimeoutMS    int    `json:"writeTimeout"`
	ForwardSize       int    `json:"forwardSize"`
	ForwardIntervalMS int    `json:"forwardInterval"`
}

func (cfg configSocks) ReadTimeout() time.Duration {
	return time.Duration(cfg.ReadTimeoutMS) * time.Millisecond
}

func (cfg configSocks) WriteTimeout() time.Duration {
	return time.Duration(cfg.WriteTimeoutMS) * time.Millisecond
}

func (cfg configSocks) ForwardInterval() time.Duration {
	return time.Duration(cfg.ForwardIntervalMS) * time.Millisecond
}

type configAPI struct {
	TimeoutMS  int `json:"timeout"`
	IntervalMS int `json:"interval"`
}

func (cfg configAPI) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

func (cfg configAPI) Interval() time.Duration {
	return time.Duration(cfg.IntervalMS) * time.Millisecond
}

type configClub struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	AccessToken string `json:"accessToken"`
	AlbumID     string `json:"albumID"`
}

type configUser struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	AccessToken string `json:"accessToken"`
}

type configQR struct {
	ZBarPath      string `json:"zbarPath"`
	ZBarTimeoutMS int    `json:"zbarTimeout"`
	ImageSize     int    `json:"imageSize"`
	ImageLevel    int    `json:"imageLevel"`
	MergeSize     int    `json:"mergeSize"`
	SaveDir       string `json:"saveDir"`
}

func (cfg configQR) ZBarTimeout() time.Duration {
	return time.Duration(cfg.ZBarTimeoutMS) * time.Millisecond
}

func defaultConfig() config {
	return config{
		Log: configLog{
			Level: 0,
		},
		Session: configSession{
			QueueSize: 500,
			TimeoutMS: 30 * 1000,
		},
		Socks: configSocks{
			ListenHost:        "127.0.0.1",
			ListenPort:        1080,
			ReadSize:          4096,
			ReadTimeoutMS:     10 * 1000,
			WriteTimeoutMS:    10 * 1000,
			ForwardSize:       3000,
			ForwardIntervalMS: 300,
		},
		API: configAPI{
			TimeoutMS:  7 * 1000,
			IntervalMS: 55,
		},
		QR: configQR{
			ZBarPath:      "/usr/local/bin/zbarimg",
			ZBarTimeoutMS: 5 * 1000,
			ImageSize:     512,
			MergeSize:     2000,
			ImageLevel:    1,
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
		key, err := hexToKey(cfg.Session.Secret)

		if err != nil {
			return config{}, fmt.Errorf("secret to key: %v", err)
		}

		cfg.Session.SecretKey = key
	}

	return cfg, nil
}

func validateConfig(cfg config) error {
	if len(cfg.Clubs) == 0 {
		return errors.New("clubs is missing")
	}

	if len(cfg.Users) == 0 {
		return errors.New("users is missing")
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
	}

	for _, user := range cfg.Users {
		if user.Name == "" {
			return errors.New("user.name is missing")
		}

		if user.ID == "" {
			return errors.New("user.id is missing")
		}

		if user.AccessToken == "" {
			return errors.New("user.accessToken is missing")
		}
	}

	if len(cfg.Session.SecretKey) == 0 {
		return errors.New("session.secret is missing")
	}

	return nil
}

func validateQR(cfg config) error {
	content := "test"
	data, err := encodeQR(cfg.QR, content)

	if err != nil {
		return err
	}

	file, err := saveQR(cfg.QR, data, "png")

	if err != nil {
		return err
	}

	defer os.Remove(file)

	decoded, err := decodeQR(cfg.QR, file)

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

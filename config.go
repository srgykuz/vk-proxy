package main

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

type config struct {
	Log     configLog     `json:"log"`
	Session configSession `json:"session"`
	Socks   configSocks   `json:"socks"`
	API     configAPI     `json:"api"`
	QR      configQR      `json:"qr"`
}

type configLog struct {
	Level   int  `json:"level"`
	Payload bool `json:"payload"`
}

type configSession struct {
	ClearIntervalMS int `json:"clearInterval"`
	QueueSize       int `json:"queueSize"`
}

func (cfg configSession) ClearInterval() time.Duration {
	return time.Duration(cfg.ClearIntervalMS) * time.Millisecond
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
	TimeoutMS       int    `json:"timeout"`
	IntervalMS      int    `json:"interval"`
	UserID          string `json:"userID"`
	UserAccessToken string `json:"userAccessToken"`
	ClubID          string `json:"clubID"`
	ClubAccessToken string `json:"clubAccessToken"`
	AlbumID         string `json:"albumID"`
}

func (cfg configAPI) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

func (cfg configAPI) Interval() time.Duration {
	return time.Duration(cfg.IntervalMS) * time.Millisecond
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
			ClearIntervalMS: 900 * 1000,
			QueueSize:       500,
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

	return cfg, nil
}

func validateConfig(cfg config) error {
	if cfg.API.UserID == "" {
		return errors.New("api.userID is missing")
	}

	if cfg.API.UserAccessToken == "" {
		return errors.New("api.userAccessToken is missing")
	}

	if cfg.API.ClubID == "" {
		return errors.New("api.clubID is missing")
	}

	if cfg.API.ClubAccessToken == "" {
		return errors.New("api.clubAccessToken is missing")
	}

	if cfg.API.AlbumID == "" {
		return errors.New("api.albumID is missing")
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

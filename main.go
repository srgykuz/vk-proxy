package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to configuration file")

	flag.Parse()

	cfg, err := parseConfig(*cfgPath)

	if err != nil {
		fmt.Fprintln(os.Stderr, "parse config:", err)
		os.Exit(1)
	}

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "invalid config:", err)
		os.Exit(1)
	}

	if err := validateQR(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "validate qr:", err)
		os.Exit(1)
	}

	slog.SetLogLoggerLevel(slog.Level(cfg.Log.Level))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenSocks(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "listen socks:", err)
			os.Exit(1)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenChat(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "listen chat:", err)
			os.Exit(1)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenWall(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "listen wall:", err)
			os.Exit(1)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := clearSessions(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "clear sessions:", err)
			os.Exit(1)
		}
	}()

	wg.Wait()
}

type config struct {
	Log     configLog     `json:"log"`
	Session configSession `json:"session"`
	Socks   configSocks   `json:"socks"`
	Chat    configChat    `json:"chat"`
	API     configAPI     `json:"api"`
	QR      configQR      `json:"qr"`
}

type configLog struct {
	Level   int  `json:"level"`
	Payload bool `json:"payload"`
}

type configSession struct {
	ClearIntervalMS int `json:"clearInterval"`
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

type configChat struct {
	CheckIntervalMS int `json:"checkInterval"`
	FetchCount      int `json:"fetchCount"`
	FetchOffset     int `json:"fetchOffset"`
}

func (cfg configChat) CheckInterval() time.Duration {
	return time.Duration(cfg.CheckIntervalMS) * time.Millisecond
}

type configAPI struct {
	TimeoutMS       int    `json:"timeout"`
	IntervalMS      int    `json:"interval"`
	UserID          string `json:"userID"`
	UserAccessToken string `json:"userAccessToken"`
	ClubID          string `json:"clubID"`
	ClubAccessToken string `json:"clubAccessToken"`
}

func (cfg configAPI) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

func (cfg configAPI) Interval() time.Duration {
	return time.Duration(cfg.IntervalMS) * time.Millisecond
}

type configQR struct {
	ZBarPath string `json:"zbarPath"`
}

func defaultConfig() config {
	return config{
		Log: configLog{
			Level: 0,
		},
		Session: configSession{
			ClearIntervalMS: 900 * 1000,
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
		Chat: configChat{
			CheckIntervalMS: 1000,
			FetchCount:      10,
		},
		API: configAPI{
			TimeoutMS:  7 * 1000,
			IntervalMS: 55,
		},
		QR: configQR{
			ZBarPath: "/usr/local/bin/zbarimg",
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

	return nil
}

func validateQR(cfg config) error {
	encoded := "test"
	png, err := encodeQR(encoded)

	if err != nil {
		return err
	}

	file, err := saveQR(png, "png")

	if err != nil {
		return err
	}

	defer os.Remove(file)

	decoded, err := decodeQR(cfg, file)

	if err != nil {
		return err
	}

	if encoded != decoded {
		return errors.New("encoded and decoded data mismatch")
	}

	return nil
}

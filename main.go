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

	slog.SetLogLoggerLevel(cfg.Log.Level)

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

	wg.Wait()
}

type config struct {
	Log   configLog   `json:"log"`
	Socks configSocks `json:"socks"`
	API   configAPI   `json:"api"`
	Chat  configChat  `json:"chat"`
}

type configLog struct {
	Level   slog.Level `json:"level"`
	Payload bool       `json:"payload"`
}

type configSocks struct {
	ListenHost         string        `json:"listenHost"`
	ListenPort         uint16        `json:"listenPort"`
	ConnectionDeadline time.Duration `json:"connectionDeadline"`
	BufferSize         int           `json:"bufferSize"`
}

type configAPI struct {
	Origin          string        `json:"origin"`
	Version         string        `json:"version"`
	Timeout         time.Duration `json:"timeout"`
	UserID          string        `json:"userID"`
	ClubAccessToken string        `json:"clubAccessToken"`
}

type configChat struct {
	CheckInterval time.Duration `json:"checkInterval"`
	FetchCount    int           `json:"fetchCount"`
	FetchOffset   int           `json:"fetchOffset"`
}

func defaultConfig() config {
	return config{
		Socks: configSocks{
			ListenHost:         "127.0.0.1",
			ListenPort:         1080,
			ConnectionDeadline: 30 * time.Second,
			BufferSize:         2048,
		},
		API: configAPI{
			Origin:  "https://api.vk.ru",
			Version: "5.199",
			Timeout: 10 * time.Second,
		},
		Chat: configChat{
			CheckInterval: 1 * time.Second,
			FetchCount:    5,
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

	if cfg.API.ClubAccessToken == "" {
		return errors.New("api.clubAccessToken is missing")
	}

	return nil
}

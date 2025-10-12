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

	wg.Wait()
}

type config struct {
	Log   configLog   `json:"log"`
	Socks configSocks `json:"socks"`
	API   configAPI   `json:"api"`
	Chat  configChat  `json:"chat"`
}

type configLog struct {
	Level   int  `json:"level"`
	Payload bool `json:"payload"`
}

type configSocks struct {
	ListenHost           string `json:"listenHost"`
	ListenPort           uint16 `json:"listenPort"`
	ConnectionDeadlineMS int    `json:"connectionDeadline"`
	BufferSize           int    `json:"bufferSize"`
	ChunkMaxSize         int    `json:"chunkMaxSize"`
}

func (cfg configSocks) ConnectionDeadline() time.Duration {
	return time.Duration(cfg.ConnectionDeadlineMS) * time.Millisecond
}

type configAPI struct {
	Origin          string `json:"origin"`
	Version         string `json:"version"`
	TimeoutMS       int    `json:"timeout"`
	UserID          string `json:"userID"`
	ClubAccessToken string `json:"clubAccessToken"`
}

func (cfg configAPI) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type configChat struct {
	CheckIntervalMS int `json:"checkInterval"`
	FetchCount      int `json:"fetchCount"`
	FetchOffset     int `json:"fetchOffset"`
}

func (cfg configChat) CheckInterval() time.Duration {
	return time.Duration(cfg.CheckIntervalMS) * time.Millisecond
}

func defaultConfig() config {
	return config{
		Log: configLog{
			Level: 0,
		},
		Socks: configSocks{
			ListenHost:           "127.0.0.1",
			ListenPort:           1080,
			ConnectionDeadlineMS: 15000,
			BufferSize:           4096,
			ChunkMaxSize:         3000,
		},
		API: configAPI{
			Origin:    "https://api.vk.ru",
			Version:   "5.199",
			TimeoutMS: 7000,
		},
		Chat: configChat{
			CheckIntervalMS: 1000,
			FetchCount:      10,
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

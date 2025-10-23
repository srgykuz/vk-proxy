package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"
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

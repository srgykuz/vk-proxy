package main

import (
	"flag"
	"fmt"
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

	if err := validateQR(cfg.QR); err != nil {
		fmt.Fprintln(os.Stderr, "validate qr:", err)
		os.Exit(1)
	}

	if err := configureLogger(cfg.Log); err != nil {
		fmt.Fprintln(os.Stderr, "configure logger:", err)
		os.Exit(1)
	}

	if err := initSession(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "init session:", err)
		os.Exit(1)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenSocks(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "listen socks:", err)
			os.Exit(1)
		}
	}()

	for _, club := range cfg.Clubs {
		wg.Add(1)
		go func(club configClub) {
			defer wg.Done()

			if err := listenLongPoll(cfg, club); err != nil {
				fmt.Fprintln(os.Stderr, "listen long poll:", err)
				os.Exit(1)
			}
		}(club)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := clearHandler(); err != nil {
			fmt.Fprintln(os.Stderr, "clear handler:", err)
			os.Exit(1)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := clearSession(); err != nil {
			fmt.Fprintln(os.Stderr, "clear sessions:", err)
			os.Exit(1)
		}
	}()

	wg.Wait()
}

package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
)

func main() {
	var cfgPath string
	var printVersion bool
	var genSecret bool

	flag.StringVar(&cfgPath, "config", "config.json", "path to configuration file")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.BoolVar(&genSecret, "secret", false, "generate secret")

	flag.Parse()

	if printVersion {
		fmt.Fprintln(os.Stdout, "0.10")
		os.Exit(0)
	}

	if genSecret {
		secret, err := generateSecret()

		if err != nil {
			fmt.Fprintln(os.Stderr, "generate secret:", err)
			os.Exit(1)
		}

		fmt.Fprintln(os.Stdout, secret)
		os.Exit(0)
	}

	cfg, err := parseConfig(cfgPath)

	if err != nil {
		fmt.Fprintln(os.Stderr, "parse config:", err)
		os.Exit(1)
	}

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "validate config:", err)
		os.Exit(1)
	}

	if err := validateQR(cfg.QR); err != nil {
		fmt.Fprintln(os.Stderr, "validate qr:", err)
		os.Exit(1)
	}

	for _, club := range cfg.Clubs {
		if err := validateClub(cfg.API, club); err != nil {
			fmt.Fprintln(os.Stderr, "validate club:", club.Name+":", err)
			os.Exit(1)
		}

		if err := validateLongPoll(cfg.API, club); err != nil {
			fmt.Fprintln(os.Stderr, "validate long poll:", club.Name+":", err)
			os.Exit(1)
		}
	}

	for _, user := range cfg.Users {
		if err := validateUser(cfg.API, user); err != nil {
			fmt.Fprintln(os.Stderr, "validate user:", user.Name+":", err)
			os.Exit(1)
		}
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

		wg.Add(1)
		go func(club configClub) {
			defer wg.Done()

			if err := listenStorage(cfg, club); err != nil {
				fmt.Fprintln(os.Stderr, "listen storage:", err)
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
			fmt.Fprintln(os.Stderr, "clear session:", err)
			os.Exit(1)
		}
	}()

	wg.Wait()
}

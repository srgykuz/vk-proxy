package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 100)
	code := 0

	go func() {
		for {
			err := <-errs
			fmt.Fprintln(os.Stderr, err)
			code = 1
			cancel()
		}
	}()

	if err := run(ctx, errs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}

	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stdout, "\nPress Enter to exit...")
		fmt.Scanln()
	}

	os.Exit(code)
}

func run(ctx context.Context, errs chan<- error) error {
	var cfgPath string
	var printVersion bool
	var genSecret bool

	flag.StringVar(&cfgPath, "config", "config.json", "path to configuration file")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.BoolVar(&genSecret, "secret", false, "generate secret")

	flag.Parse()

	if printVersion {
		fmt.Fprintln(os.Stdout, "0.10")

		return nil
	}

	if genSecret {
		secret, err := generateSecret()

		if err != nil {
			return fmt.Errorf("generate secret: %v", err)
		}

		fmt.Fprintln(os.Stdout, secret)

		return nil
	}

	cfg, err := parseConfig(cfgPath)

	if err != nil {
		return fmt.Errorf("parse config: %v", err)
	}

	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("validate config: %v", err)
	}

	if err := configureLogger(cfg.Log); err != nil {
		return fmt.Errorf("configure logger: %v", err)
	}

	if err := configureDNS(cfg.DNS); err != nil {
		return fmt.Errorf("configure dns: %v", err)
	}

	if err := validateQR(cfg.QR); err != nil {
		return fmt.Errorf("validate qr: %v", err)
	}

	for _, club := range cfg.Clubs {
		if err := validateClub(cfg.API, club); err != nil {
			return fmt.Errorf("validate club: %v: %v", club.Name, err)
		}

		if err := validateLongPoll(cfg.API, club); err != nil {
			return fmt.Errorf("validate long poll: %v: %v", club.Name, err)
		}
	}

	for _, user := range cfg.Users {
		if err := validateUser(cfg.API, user); err != nil {
			return fmt.Errorf("validate user: %v: %v", user.Name, err)
		}
	}

	if err := initSession(cfg); err != nil {
		return fmt.Errorf("init session: %v", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenSocks(ctx, cfg); err != nil {
			errs <- fmt.Errorf("listen socks: %v", err)
		}
	}()

	for _, club := range cfg.Clubs {
		wg.Add(1)
		go func(club configClub) {
			defer wg.Done()

			if err := listenLongPoll(ctx, cfg, club); err != nil {
				errs <- fmt.Errorf("listen long poll: %v", err)
			}
		}(club)

		wg.Add(1)
		go func(club configClub) {
			defer wg.Done()

			if err := listenStorage(ctx, cfg, club); err != nil {
				errs <- fmt.Errorf("listen storage: %v", err)
			}
		}(club)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := clearHandler(ctx); err != nil {
			errs <- fmt.Errorf("clear handler: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := clearSession(ctx); err != nil {
			errs <- fmt.Errorf("clear session: %v", err)
		}
	}()

	wg.Wait()

	return nil
}

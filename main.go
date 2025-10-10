package main

import (
	"log/slog"
	"os"
	"strconv"
	"sync"
)

var socksHost = os.Getenv("SOCKS_HOST")
var socksPort = os.Getenv("SOCKS_PORT")

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	if socksHost == "" {
		socksHost = "127.0.0.1"
	}

	if socksPort == "" {
		socksPort = "1080"
	}

	socksPortI, err := strconv.Atoi(socksPort)

	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenSocks(socksHost, uint16(socksPortI)); err != nil {
			panic(err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := listenChat(); err != nil {
			panic(err)
		}
	}()

	wg.Wait()
}

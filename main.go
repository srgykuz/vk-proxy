package main

import (
	"log/slog"
	"os"
	"sync"
)

var mode = os.Getenv("MODE")

func main() {
	var wg sync.WaitGroup

	slog.SetLogLoggerLevel(slog.LevelDebug)

	if mode == "client" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listenSocks()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		listenChat()
	}()

	wg.Wait()
}

package main

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

var mode = os.Getenv("MODE")

func main() {
	var wg sync.WaitGroup
	conns := map[string]*vkConn{}

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
		listenChat(conns)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			time.Sleep(time.Second * 30)
			clearVkConns(conns)
		}
	}()

	wg.Wait()
}

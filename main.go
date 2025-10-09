package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var MODE = os.Getenv("MODE")

func main() {
	var wg sync.WaitGroup
	vkConns := map[string]*vkConn{}

	if MODE == "client" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listenSocks(vkConns)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		listenChat(vkConns)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			time.Sleep(time.Second * 30)

			for id, vk := range vkConns {
				if vk.closed {
					delete(vkConns, id)
					fmt.Printf("vkConn id %v: removed\n", id)
				}
			}
		}
	}()

	wg.Wait()
}

package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"
)

func listenChat(conns map[string]*vkConn) {
	lastMsg, err := messagesGetLatest()

	if err != nil {
		panic(err)
	}

	for {
		time.Sleep(time.Second * 1)

		p := messagesGetHistoryParams{
			offset: lastMsg.ID,
			count:  5,
			rev:    1,
		}
		msgs, err := messagesGetHistory(p)

		if err != nil {
			panic(err)
		}

		if len(msgs.Items) > 0 {
			lastMsg = msgs.Items[len(msgs.Items)-1]
		}

		for _, msg := range msgs.Items {
			parts := strings.Split(msg.Text, " ")

			if parts[0] == MODE {
				continue
			} else if parts[0] != "server" && parts[0] != "client" {
				fmt.Println("Unknown mode:", msg.Text)
				continue
			}

			id := parts[1]

			if _, exists := conns[id]; !exists {
				vk, err := openVkConn(id)

				if err != nil {
					fmt.Println("Failed to open vkConn:", err)
					continue
				}

				conns[id] = vk
			}

			vk := conns[id]

			handleMessage(msg.Text, vk)
		}
	}
}

func handleMessage(msg string, vk *vkConn) {
	if vk.closed {
		fmt.Printf("vkConn id %v: is closed, ignoring message\n", vk.id)
		return
	}

	parts := strings.Split(msg, " ")
	cmd := parts[2]

	switch cmd {
	case "CONNECT":
		fmt.Printf("vkConn id %v: received connect command\n", vk.id)

		host, port := parts[3], parts[4]
		c, err := net.Dial("tcp", net.JoinHostPort(host, port))

		if err != nil {
			vk.error(err)
			return
		}

		vk.fwdConn = c
		go handleSocks(c, vk, false)

		vk.connected()
	case "ERROR":
		fmt.Printf("vkConn id %v: received error: %v\n", vk.id, strings.Join(parts[3:], " "))
	case "CONNECTED":
		fmt.Printf("vkConn id %v: connection established\n", vk.id)
		vk.established <- struct{}{}
	case "FORWARD":
		data, err := base64.StdEncoding.DecodeString(parts[3])

		if err != nil {
			fmt.Printf("vkConn id %v: failed to decode data: %v\n", vk.id, err)
			return
		}

		fmt.Printf("vkConn id %v: forwarding %d bytes\n", vk.id, len(data))

		if _, err := vk.fwdConn.Write(data); err != nil {
			fmt.Printf("vkConn id %v: failed to forward data: %v\n", vk.id, err)
			vk.error(err)
			return
		}
	case "CLOSE":
		vk.close()
	default:
		fmt.Printf("vkConn id %v: unknown command: %v\n", vk.id, cmd)
	}
}

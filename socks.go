package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
)

func listenSocks(conns map[string]*vkConn) {
	ln, err := net.Listen("tcp", "127.0.0.1:1080")

	if err != nil {
		panic(err)
	}

	fmt.Println("Listening at 127.0.0.1:1080")

	for {
		conn, err := ln.Accept()

		if err != nil {
			panic(err)
		}

		vk, err := openVkConn("")

		if err != nil {
			panic(err)
		}

		conns[vk.id] = vk
		vk.fwdConn = conn

		go handleSocks(conn, vk, true)
	}
}

func handleSocks(conn net.Conn, vk *vkConn, socks bool) {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn.Close()
		defer vk.close()

		buf := make([]byte, 2048)
		step := 1

		if !socks {
			step = 3
		}

		for {
			n, err := conn.Read(buf)

			if n > 0 {
				data := buf[:n]

				// fmt.Println(data)

				switch step {
				case 1:
					conn.Write([]byte{0x05, 0x00})
				case 2:
					cmd := data[1]

					if cmd != 0x01 {
						fmt.Println("Not a CONNECT command")
						return
					}

					atyp := data[3]

					if atyp != 0x01 {
						fmt.Println("Not an IPv4 address")
						return
					}

					host := net.IPv4(data[4], data[5], data[6], data[7]).String()
					port := int(binary.BigEndian.Uint16(data[8:10]))

					vk.connect(host, port)
					<-vk.established

					conn.Write([]byte{0x05, 0x00, 0x00, 0x01, data[4], data[5], data[6], data[7], data[8], data[9]})
				default:
					vk.forward(data)
				}

				step++
			}

			if err != nil {
				if err.Error() != "EOF" {
					fmt.Println("Read error: ", err)
				} else {
					fmt.Println("Connection closed by peer")
				}

				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn.Close()
		defer vk.close()

		<-vk.terminated
	}()

	wg.Wait()
}

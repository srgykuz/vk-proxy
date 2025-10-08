package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var ACCESS_TOKEN = os.Getenv("ACCESS_TOKEN")
var USER_ID = os.Getenv("USER_ID")
var MODE = os.Getenv("MODE")
var API_VERSION = "5.199"

func main() {
	var wg sync.WaitGroup
	vkConns := map[string]*vkConn{}

	if MODE == "client" {
		wg.Add(1)
		go func() {
			defer wg.Done()

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

				vkConns[vk.id] = vk
				vk.fwdConn = conn

				go handleConn(conn, vk, true)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		lastMsg, err := getLastMessage()

		if err != nil {
			panic(err)
		}

		for {
			time.Sleep(time.Second * 1)

			p := GetMessagesParams{
				Offset: lastMsg.ID,
				Count:  5,
				Rev:    1,
			}
			msgs, err := getMessages(p)

			if err != nil {
				panic(err)
			}

			if len(msgs) > 0 {
				lastMsg = msgs[len(msgs)-1]
			}

			for _, msg := range msgs {
				parts := strings.Split(msg.Text, " ")

				if parts[0] == MODE {
					continue
				} else if parts[0] != "server" && parts[0] != "client" {
					fmt.Println("Unknown mode:", msg.Text)
					continue
				}

				id := parts[1]

				if _, exists := vkConns[id]; !exists {
					vk, err := openVkConn(id)

					if err != nil {
						fmt.Println("Failed to open vkConn:", err)
						continue
					}

					vkConns[id] = vk
				}

				vk := vkConns[id]

				handleMessage(msg.Text, vk)
			}
		}
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

var vkConnID = 0
var vkConnIDMu sync.Mutex

type vkConn struct {
	id          string
	receive     chan string
	send        chan string
	established chan struct{}
	terminated  chan struct{}
	closed      bool
	fwdConn     net.Conn
	closeMu     sync.Mutex
}

func openVkConn(id string) (*vkConn, error) {
	if id == "" {
		vkConnIDMu.Lock()
		vkConnID++
		id = fmt.Sprint(vkConnID)
		vkConnIDMu.Unlock()
	}

	c := &vkConn{
		id:          id,
		receive:     make(chan string, 100),
		send:        make(chan string, 100),
		established: make(chan struct{}),
		terminated:  make(chan struct{}),
		closed:      false,
		fwdConn:     nil,
		closeMu:     sync.Mutex{},
	}

	go func() {
		for msg := range c.send {
			if err := sendMessage(msg); err != nil {
				fmt.Printf("vkConn id %v: failed to send message: %v\n", c.id, err)
				continue
			}
		}
	}()

	fmt.Printf("vkConn id %v: opened\n", c.id)

	return c, nil
}

func (c *vkConn) close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.closed {
		return nil
	}

	msg := fmt.Sprintf("%v %v CLOSE", MODE, c.id)
	c.send <- msg

	close(c.send)
	close(c.receive)
	close(c.established)
	close(c.terminated)

	if c.fwdConn != nil {
		c.fwdConn.Close()
	}

	c.closed = true

	fmt.Printf("vkConn id %v: closed\n", c.id)

	return nil
}

func (c *vkConn) connect(host string, port int) {
	msg := fmt.Sprintf("%v %v CONNECT %v %v", MODE, c.id, host, port)
	c.send <- msg
}

func (c *vkConn) connected() {
	msg := fmt.Sprintf("%v %v CONNECTED", MODE, c.id)
	c.send <- msg
}

func (c *vkConn) error(err error) {
	msg := fmt.Sprintf("%v %v ERROR %v", MODE, c.id, err.Error())
	c.send <- msg
}

func (c *vkConn) forward(data []byte) {
	msg := fmt.Sprintf("%v %v FORWARD %v", MODE, c.id, base64.StdEncoding.EncodeToString(data))
	c.send <- msg
}

func handleConn(conn net.Conn, vk *vkConn, socks bool) {
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
		go handleConn(c, vk, false)

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

type VKError struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

type SendMessageResp struct {
	Error    VKError `json:"error"`
	Response int     `json:"response"`
}

func sendMessage(msg string) error {
	u, err := url.Parse("https://api.vk.ru/method/messages.send")

	if err != nil {
		return err
	}

	q := url.Values{
		"user_id":      []string{USER_ID},
		"access_token": []string{ACCESS_TOKEN},
		"v":            []string{API_VERSION},
		"random_id":    []string{"0"},
		"message":      []string{msg},
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("POST", u.String(), nil)

	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return err
	}

	body := SendMessageResp{}

	if err := json.Unmarshal(data, &body); err != nil {
		return fmt.Errorf("failed to unmarshal response json: %w; body: %s", err, string(data))
	}

	if body.Error.ErrorCode != 0 {
		return fmt.Errorf("VK API error %d: %s", body.Error.ErrorCode, body.Error.ErrorMsg)
	}

	return nil
}

type GetMessagesResp struct {
	Error    VKError             `json:"error"`
	Response GetMessagesResponse `json:"response"`
}

type GetMessagesResponse struct {
	Count int       `json:"count"`
	Items []Message `json:"items"`
}

type Message struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

type GetMessagesParams struct {
	Offset int
	Count  int
	Rev    int
}

func getMessages(params GetMessagesParams) ([]Message, error) {
	u, err := url.Parse("https://api.vk.ru/method/messages.getHistory")

	if err != nil {
		return nil, err
	}

	q := url.Values{
		"user_id":      []string{USER_ID},
		"access_token": []string{ACCESS_TOKEN},
		"v":            []string{API_VERSION},
		"offset":       []string{fmt.Sprintf("%d", params.Offset)},
		"count":        []string{fmt.Sprintf("%d", params.Count)},
		"rev":          []string{fmt.Sprintf("%d", params.Rev)},
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequest("POST", u.String(), nil)

	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	body := GetMessagesResp{}

	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response json: %w; body: %s", err, string(data))
	}

	if body.Error.ErrorCode != 0 {
		return nil, fmt.Errorf("VK API error %d: %s", body.Error.ErrorCode, body.Error.ErrorMsg)
	}

	return body.Response.Items, nil
}

func getLastMessage() (Message, error) {
	params := GetMessagesParams{
		Offset: 0,
		Count:  1,
		Rev:    0,
	}
	messages, err := getMessages(params)

	if err != nil {
		return Message{}, err
	}

	if len(messages) == 0 {
		return Message{}, fmt.Errorf("no messages found")
	}

	return messages[0], nil
}

package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"sync"
)

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
			p := messagesSendParams{
				message: msg,
			}

			if _, err := messagesSend(p); err != nil {
				fmt.Printf("vkConn id %v: failed to send message: %v\n", c.id, err)
				continue
			}
		}
	}()

	fmt.Printf("vkConn id %v: opened\n", c.id)

	return c, nil
}

func clearVkConns(conns map[string]*vkConn) {
	for id, vk := range conns {
		if vk.closed {
			delete(conns, id)
			fmt.Printf("vkConn id %v: removed\n", id)
		}
	}
}

func (c *vkConn) close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.closed {
		return nil
	}

	msg := fmt.Sprintf("%v %v CLOSE", mode, c.id)
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
	msg := fmt.Sprintf("%v %v CONNECT %v %v", mode, c.id, host, port)
	c.send <- msg
}

func (c *vkConn) connected() {
	msg := fmt.Sprintf("%v %v CONNECTED", mode, c.id)
	c.send <- msg
}

func (c *vkConn) error(err error) {
	msg := fmt.Sprintf("%v %v ERROR %v", mode, c.id, err.Error())
	c.send <- msg
}

func (c *vkConn) forward(data []byte) {
	msg := fmt.Sprintf("%v %v FORWARD %v", mode, c.id, base64.StdEncoding.EncodeToString(data))
	c.send <- msg
}

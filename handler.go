package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"sort"
	"sync"
	"time"
)

func listenLongPoll(cfg config) error {
	server, err := groupsGetLongPollServer(cfg)

	if err != nil {
		return err
	}

	last := groupsUseLongPollServerResponse{
		TS: server.TS,
	}

	slog.Info("long poll: listening")

	for {
		last, err = groupsUseLongPollServer(cfg, server, last)

		if err != nil {
			slog.Error("long poll: listen", "err", err)
			continue
		}

		if last.Failed != 0 {
			slog.Debug("long poll: refresh")

			server, err = groupsGetLongPollServer(cfg)

			if err == nil {
				last = groupsUseLongPollServerResponse{
					TS: server.TS,
				}
			} else {
				slog.Error("long poll: refresh", "err", err)
			}

			continue
		}

		for _, upd := range last.Updates {
			go func() {
				if err := handleUpdate(cfg, upd); err != nil {
					slog.Error("handler: update", "type", upd.Type, "err", err)
				}
			}()
		}
	}
}

func handleUpdate(cfg config, upd update) error {
	var encodedS string
	var encodedB []byte
	var datagrams []datagram
	var err error

	switch upd.TypeEnum() {
	case updateTypeMessageReply:
		encodedS = upd.Object.Text
	case updateTypeWallPostNew:
		encodedS = upd.Object.Text
	case updateTypeWallReplyNew:
		encodedS = upd.Object.Text
	case updateTypePhotoNew:
		if shouldHandlePhoto(upd.Object.Text) {
			datagrams, err = handlePhoto(cfg, upd.Object.OrigPhoto.URL)
		}
	case updateTypeGroupChangeSettings:
		if shouldHandleDoc(upd.Object.Changes.Website.NewValue) {
			uri := clearDocURL(upd.Object.Changes.Website.NewValue)
			encodedB, err = apiDownloadURL(cfg, uri)
		}
	default:
		err = errors.New("unsupported update")
	}

	if err != nil {
		return err
	}

	if len(encodedB) > 0 {
		encodedS = string(encodedB)
	}

	if len(encodedS) > 0 {
		dg, err := handleEncoded(encodedS)

		if err != nil {
			return err
		}

		if !dg.isZero() {
			datagrams = append(datagrams, dg)
		}
	}

	for _, dg := range datagrams {
		slog.Debug("handler: update", "type", upd.Type, "dg", dg)

		if err := handleDatagram(cfg, dg); err != nil {
			slog.Error("handler: update", "type", upd.Type, "dg", dg, "err", err)
		}
	}

	return nil
}

func shouldHandlePhoto(caption string) bool {
	if len(caption) == 0 {
		return true
	}

	dg, err := handleEncoded(caption)

	if err != nil {
		return true
	}

	return !dg.isZero()
}

func shouldHandleDoc(uri string) bool {
	parsed, err := url.Parse(uri)

	if err != nil {
		return true
	}

	caption := parsed.Query().Get("caption")

	if len(caption) == 0 {
		return true
	}

	dg, err := handleEncoded(caption)

	if err != nil {
		return true
	}

	return !dg.isZero()
}

func clearDocURL(uri string) string {
	parsed, err := url.Parse(uri)

	if err != nil {
		return uri
	}

	q := parsed.Query()
	q.Del("caption")

	parsed.RawQuery = q.Encode()

	return parsed.String()
}

func handlePhoto(cfg config, url string) ([]datagram, error) {
	b, err := apiDownloadURL(cfg, url)

	if err != nil {
		return nil, fmt.Errorf("download url: %v", err)
	}

	file, err := saveQR(cfg.QR, b, "jpg")

	if err != nil {
		return nil, fmt.Errorf("save qr: %v", err)
	}

	defer os.Remove(file)

	content, err := decodeQR(cfg.QR, file)

	if err != nil {
		return nil, fmt.Errorf("decode qr: %v", err)
	}

	datagrams := []datagram{}

	for _, s := range content {
		dg, err := handleEncoded(s)

		if err != nil {
			return nil, err
		}

		if !dg.isZero() {
			datagrams = append(datagrams, dg)
		}
	}

	sort.Slice(datagrams, func(i, j int) bool {
		return datagrams[i].number < datagrams[j].number
	})

	return datagrams, nil
}

func handleEncoded(s string) (datagram, error) {
	dg, err := decodeDatagram(s)

	if err != nil {
		return datagram{}, fmt.Errorf("decode datagram: %v", err)
	}

	if dg.isLoopback() {
		return datagram{}, nil
	}

	return dg, nil
}

var handleDatagramMu *sync.Mutex = &sync.Mutex{}
var handleDatagramQueues map[dgSes]*handlerPriorityQueue = map[dgSes]*handlerPriorityQueue{}

func handleDatagram(cfg config, dg datagram) error {
	handleDatagramMu.Lock()
	defer handleDatagramMu.Unlock()

	ses, exists := getSession(dg.session)

	if !exists {
		var err error
		ses, err = openSession(dg.session, cfg)

		if err != nil {
			return fmt.Errorf("open session: %v", err)
		}

		setSession(ses.id, ses)
		delete(handleDatagramQueues, ses.id)
	}

	queue, exists := handleDatagramQueues[ses.id]

	if !exists {
		queue = openHandlerPriorityQueue(cfg, ses)
		handleDatagramQueues[ses.id] = queue
	}

	if err := queue.add(dg); err != nil {
		return fmt.Errorf("queue add: %v", err)
	}

	return nil
}

func handleCommand(cfg config, ses *session, dg datagram) error {
	slog.Debug("handler: command", "dg", dg)

	if cfg.Log.Payload {
		slog.Debug("handler: payload", "ses", ses, "in", bytesToHex(dg.payload))
	}

	var err error

	switch dg.command {
	case commandConnect:
		err = handleConnect(cfg, ses, dg)

		if err == nil {
			slog.Info("handler: forwarding", "ses", ses)
		}
	case commandForward:
		err = handleForward(ses, dg)
	case commandClose:
		handleClose(ses)
	case commandRetry:
		err = handleRetry(ses, dg)
	default:
		err = errors.New("unsupported")
	}

	if err != nil {
		return fmt.Errorf("command %v: %v", dg.command, err)
	}

	return nil
}

func handleConnect(cfg config, ses *session, dg datagram) error {
	pld := payloadConnect{}

	if err := pld.decode(dg.payload); err != nil {
		return err
	}

	addr := address(pld).String()
	timeout := 10 * time.Second
	conn, err := net.DialTimeout("tcp", addr, timeout)

	if err != nil {
		return err
	}

	ses.setPeer(conn)

	go acceptSocks(cfg, ses, stageForward)

	return nil
}

func handleForward(ses *session, dg datagram) error {
	if err := ses.writePeer(dg.payload); err != nil {
		return err
	}

	return nil
}

func handleClose(ses *session) {
	ses.close()
}

func handleRetry(ses *session, dg datagram) error {
	pld := payloadRetry{}

	if err := pld.decode(dg.payload); err != nil {
		return err
	}

	dg, exists := ses.getHistory(pld.number)

	if exists {
		if err := ses.sendDatagram(dg); err != nil {
			return err
		}
	} else {
		slog.Debug("handler: history miss", "ses", ses, "number", pld.number)
	}

	return nil
}

type handlerPriorityQueue struct {
	cfg     config
	ses     *session
	mu      sync.Mutex
	closed  bool
	temp    []datagram
	data    map[dgNum]datagram
	next    dgNum
	pending dgNum
	retries int
	signal  chan struct{}
}

func openHandlerPriorityQueue(cfg config, ses *session) *handlerPriorityQueue {
	slog.Debug("handler: queue open", "ses", ses)

	q := &handlerPriorityQueue{
		cfg:     cfg,
		ses:     ses,
		mu:      sync.Mutex{},
		closed:  false,
		temp:    []datagram{},
		data:    map[dgNum]datagram{},
		next:    1,
		pending: 0,
		retries: 0,
		signal:  make(chan struct{}, 1),
	}

	go func() {
		q.listen()
		q.close()
	}()

	return q
}

func (q *handlerPriorityQueue) close() {
	slog.Debug("handler: queue close", "ses", q.ses)

	q.mu.Lock()
	defer q.mu.Unlock()

	clear(q.temp)
	clear(q.data)

	q.closed = true
}

func (q *handlerPriorityQueue) isClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.closed
}

func (q *handlerPriorityQueue) add(dg datagram) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return errors.New("queue is closed")
	}

	q.temp = append(q.temp, dg)

	select {
	case q.signal <- struct{}{}:
	default:
	}

	return nil
}

func (q *handlerPriorityQueue) listen() {
	retryInterval := 10 * time.Second

	for {
		stop := false

		select {
		case <-q.signal:
			stop = q.handle()
		case <-time.After(retryInterval):
			stop = q.retry()
		case <-q.ses.onClose:
			return
		}

		if stop {
			q.send(commandClose, nil)
			handleClose(q.ses)
			return
		}
	}
}

func (q *handlerPriorityQueue) handle() bool {
	q.mu.Lock()

	for _, dg := range q.temp {
		q.data[dg.number] = dg
	}

	q.temp = []datagram{}

	q.mu.Unlock()

	for {
		dg, exists := q.data[q.next]

		if !exists {
			break
		}

		if err := handleCommand(q.cfg, q.ses, dg); err != nil {
			slog.Error("handler: command", "dg", dg, "err", err)
			return true
		}

		q.next++
	}

	return false
}

func (q *handlerPriorityQueue) retry() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.data[q.next]; exists {
		return false
	}

	for _, dg := range q.temp {
		if dg.number == q.next {
			return false
		}
	}

	if q.next == q.pending {
		if q.retries >= 3 {
			return true
		}

		q.retries++
	} else {
		q.pending = q.next
		q.retries = 1
	}

	pld := payloadRetry{
		number: q.next,
	}
	q.send(commandRetry, pld.encode())

	return false
}

func (q *handlerPriorityQueue) send(cmd dgCmd, pld []byte) {
	dg := newDatagram(0, 0, cmd, pld)

	if err := q.ses.sendDatagram(dg); err != nil {
		slog.Error("handler: send", "ses", q.ses, "cmd", cmd, "err", err)
	}
}

func clearHandler() error {
	interval := 5 * time.Minute

	for {
		time.Sleep(interval)

		handleDatagramMu.Lock()

		for key, queue := range handleDatagramQueues {
			if queue.isClosed() {
				delete(handleDatagramQueues, key)
			}
		}

		handleDatagramMu.Unlock()
	}
}

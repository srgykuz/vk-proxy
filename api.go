package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var clubAccessToken = os.Getenv("CLUB_ACCESS_TOKEN")
var userID = os.Getenv("USER_ID")
var version = "5.199"
var origin = "https://api.vk.ru/method"

type errorResult struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (r errorResult) check() error {
	if r.ErrorCode != 0 {
		return fmt.Errorf("code %d: %s", r.ErrorCode, r.ErrorMsg)
	}

	return nil
}

func createURL(path string, values url.Values) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return fmt.Sprintf("%v%v?%s", origin, path, values.Encode())
}

func do(req *http.Request) (*http.Response, error) {
	client := &http.Client{
		Timeout: time.Second * 30,
	}

	return client.Do(req)
}

func check(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %v", resp.StatusCode)
	}

	return nil
}

type messagesSendParams struct {
	message string
}

type messagesSendResult struct {
	Error    errorResult `json:"error"`
	Response int         `json:"response"`
}

func messagesSend(params messagesSendParams) (int, error) {
	values := url.Values{}
	values.Set("access_token", clubAccessToken)
	values.Set("v", version)
	values.Set("user_id", userID)
	values.Set("random_id", "0")
	values.Set("message", params.message)

	uri := createURL("messages.send", values)
	req, err := http.NewRequest("POST", uri, nil)

	if err != nil {
		return 0, err
	}

	resp, err := do(req)

	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	if err := check(resp); err != nil {
		return 0, err
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return 0, err
	}

	result := messagesSendResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}

	if err := result.Error.check(); err != nil {
		return 0, err
	}

	return result.Response, nil
}

type messagesGetHistoryParams struct {
	offset int
	count  int
	rev    int
}

type messagesGetHistoryResult struct {
	Error    errorResult                `json:"error"`
	Response messagesGetHistoryResponse `json:"response"`
}

type messagesGetHistoryResponse struct {
	Count int       `json:"count"`
	Items []message `json:"items"`
}

type message struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func messagesGetHistory(params messagesGetHistoryParams) (messagesGetHistoryResponse, error) {
	values := url.Values{}
	values.Set("access_token", clubAccessToken)
	values.Set("v", version)
	values.Set("user_id", userID)
	values.Set("offset", fmt.Sprint(params.offset))
	values.Set("count", fmt.Sprint(params.count))
	values.Set("rev", fmt.Sprint(params.rev))

	uri := createURL("messages.getHistory", values)
	req, err := http.NewRequest("POST", uri, nil)

	if err != nil {
		return messagesGetHistoryResponse{}, err
	}

	resp, err := do(req)

	if err != nil {
		return messagesGetHistoryResponse{}, err
	}

	defer resp.Body.Close()

	if err := check(resp); err != nil {
		return messagesGetHistoryResponse{}, err
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return messagesGetHistoryResponse{}, err
	}

	result := messagesGetHistoryResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return messagesGetHistoryResponse{}, err
	}

	if err := result.Error.check(); err != nil {
		return messagesGetHistoryResponse{}, err
	}

	return result.Response, nil
}

func messagesGetLatest() (message, error) {
	p := messagesGetHistoryParams{
		offset: 0,
		count:  1,
		rev:    0,
	}
	resp, err := messagesGetHistory(p)

	if err != nil {
		return message{}, err
	}

	if len(resp.Items) == 0 {
		return message{}, fmt.Errorf("no messages found")
	}

	return resp.Items[0], nil
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

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

func createURL(cfg config, path string, values url.Values) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return fmt.Sprintf("%v%v?%s", cfg.API.Origin, path, values.Encode())
}

func do(cfg config, req *http.Request) (*http.Response, error) {
	client := &http.Client{
		Timeout: cfg.API.Timeout,
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

func messagesSend(cfg config, params messagesSendParams) (int, error) {
	values := url.Values{}
	values.Set("access_token", cfg.API.ClubAccessToken)
	values.Set("v", cfg.API.Version)
	values.Set("user_id", cfg.API.UserID)
	values.Set("random_id", "0")
	values.Set("message", params.message)

	uri := createURL(cfg, "messages.send", values)
	req, err := http.NewRequest("POST", uri, nil)

	if err != nil {
		return 0, err
	}

	resp, err := do(cfg, req)

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

func messagesGetHistory(cfg config, params messagesGetHistoryParams) (messagesGetHistoryResponse, error) {
	values := url.Values{}
	values.Set("access_token", cfg.API.ClubAccessToken)
	values.Set("v", cfg.API.Version)
	values.Set("user_id", cfg.API.UserID)
	values.Set("offset", fmt.Sprint(params.offset))
	values.Set("count", fmt.Sprint(params.count))
	values.Set("rev", fmt.Sprint(params.rev))

	uri := createURL(cfg, "messages.getHistory", values)
	req, err := http.NewRequest("POST", uri, nil)

	if err != nil {
		return messagesGetHistoryResponse{}, err
	}

	resp, err := do(cfg, req)

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

func messagesGetLatest(cfg config) (message, error) {
	p := messagesGetHistoryParams{
		offset: 0,
		count:  1,
		rev:    0,
	}
	resp, err := messagesGetHistory(cfg, p)

	if err != nil {
		return message{}, err
	}

	if len(resp.Items) == 0 {
		return message{}, fmt.Errorf("no messages found")
	}

	return resp.Items[0], nil
}

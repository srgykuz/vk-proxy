package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func apiURL(cfg config, method string, values url.Values) string {
	method = strings.TrimPrefix(method, "/")

	return fmt.Sprintf("%v/method/%v?%s", cfg.API.Origin, method, values.Encode())
}

func apiValues(cfg config) url.Values {
	return url.Values{
		"v":            []string{cfg.API.Version},
		"access_token": []string{cfg.API.ClubAccessToken},
	}
}

func apiDo(cfg config, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(req.Context(), cfg.API.Timeout())
	defer cancel()

	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %v", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("read response: %v", err)
	}

	return data, nil
}

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

type messagesSendParams struct {
	message string
}

type messagesSendResult struct {
	Error    errorResult `json:"error"`
	Response int         `json:"response"`
}

func messagesSend(cfg config, params messagesSendParams) (int, error) {
	values := apiValues(cfg)

	values.Set("user_id", cfg.API.UserID)
	values.Set("random_id", "0")
	values.Set("message", params.message)

	uri := apiURL(cfg, "messages.send", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return 0, err
	}

	data, err := apiDo(cfg, req)

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
	values := apiValues(cfg)

	values.Set("user_id", cfg.API.UserID)
	values.Set("offset", fmt.Sprint(params.offset))
	values.Set("count", fmt.Sprint(params.count))
	values.Set("rev", fmt.Sprint(params.rev))

	uri := apiURL(cfg, "messages.getHistory", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return messagesGetHistoryResponse{}, err
	}

	data, err := apiDo(cfg, req)

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
		return message{}, fmt.Errorf("chat is empty")
	}

	return resp.Items[0], nil
}

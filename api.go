package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

var ACCESS_TOKEN = os.Getenv("ACCESS_TOKEN")
var USER_ID = os.Getenv("USER_ID")
var API_VERSION = "5.199"

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

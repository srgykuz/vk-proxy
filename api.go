package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

func apiForm(form map[string]string) (io.Reader, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range form {
		if err := writer.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return body, writer.FormDataContentType(), nil
}

func apiDo(cfg config, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(req.Context(), cfg.API.Timeout())
	defer cancel()

	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		if e, ok := err.(*url.Error); ok {
			e.URL = req.URL.Path
		}

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
	form := map[string]string{
		"user_id":   cfg.API.UserID,
		"random_id": "0",
		"message":   params.message,
	}
	body, ct, err := apiForm(form)

	if err != nil {
		return 0, err
	}

	values := apiValues(cfg)
	uri := apiURL(cfg, "messages.send", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", ct)

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

type groupsGetLongPollServerResult struct {
	Error    errorResult                     `json:"error"`
	Response groupsGetLongPollServerResponse `json:"response"`
}

type groupsGetLongPollServerResponse struct {
	Key    string      `json:"key"`
	Server string      `json:"server"`
	TS     json.Number `json:"ts"`
}

func groupsGetLongPollServer(cfg config) (groupsGetLongPollServerResponse, error) {
	values := apiValues(cfg)

	values.Set("group_id", cfg.API.ClubID)

	uri := apiURL(cfg, "groups.getLongPollServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	data, err := apiDo(cfg, req)

	if err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	result := groupsGetLongPollServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	if err := result.Error.check(); err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	return result.Response, nil
}

type groupsUseLongPollServerResponse struct {
	Failed  int         `json:"failed"`
	TS      json.Number `json:"ts"`
	Updates []update    `json:"updates"`
}

type update struct {
	Type    string       `json:"type"`
	EventID string       `json:"event_id"`
	V       string       `json:"v"`
	GroupID int          `json:"group_id"`
	Object  updateObject `json:"object"`
}

type updateObject struct {
	ID   int    `json:"id"`
	Date int    `json:"date"`
	Text string `json:"text"`
}

func groupsUseLongPollServer(cfg config, server groupsGetLongPollServerResponse, last groupsUseLongPollServerResponse) (groupsUseLongPollServerResponse, error) {
	values := url.Values{}

	values.Set("act", "a_check")
	values.Set("key", server.Key)
	values.Set("ts", last.TS.String())
	values.Set("wait", "25")

	cfg.API.TimeoutMS = 30 * 1000
	uri := fmt.Sprintf("%v?%v", server.Server, values.Encode())

	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return groupsUseLongPollServerResponse{}, err
	}

	data, err := apiDo(cfg, req)

	if err != nil {
		return groupsUseLongPollServerResponse{}, err
	}

	result := groupsUseLongPollServerResponse{}

	if err := json.Unmarshal(data, &result); err != nil {
		return groupsUseLongPollServerResponse{}, err
	}

	return result, nil
}

type wallPostParams struct {
	message string
}

type wallPostResult struct {
	Error    errorResult      `json:"error"`
	Response wallPostResponse `json:"response"`
}

type wallPostResponse struct {
	PostID int `json:"post_id"`
}

func wallPost(cfg config, params wallPostParams) (wallPostResponse, error) {
	form := map[string]string{
		"owner_id": "-" + cfg.API.ClubID,
		"message":  params.message,
	}
	body, ct, err := apiForm(form)

	if err != nil {
		return wallPostResponse{}, err
	}

	values := apiValues(cfg)
	uri := apiURL(cfg, "wall.post", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return wallPostResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, req)

	if err != nil {
		return wallPostResponse{}, err
	}

	result := wallPostResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return wallPostResponse{}, err
	}

	if err := result.Error.check(); err != nil {
		return wallPostResponse{}, err
	}

	return result.Response, nil
}

type wallCreateCommentParams struct {
	postID  int
	message string
}

type wallCreateCommentResult struct {
	Error    errorResult               `json:"error"`
	Response wallCreateCommentResponse `json:"response"`
}

type wallCreateCommentResponse struct {
	CommentID int `json:"comment_id"`
}

func wallCreateComment(cfg config, params wallCreateCommentParams) (wallCreateCommentResponse, error) {
	form := map[string]string{
		"owner_id": "-" + cfg.API.ClubID,
		"post_id":  fmt.Sprint(params.postID),
		"message":  params.message,
	}
	body, ct, err := apiForm(form)

	if err != nil {
		return wallCreateCommentResponse{}, err
	}

	values := apiValues(cfg)
	uri := apiURL(cfg, "wall.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return wallCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, req)

	if err != nil {
		return wallCreateCommentResponse{}, err
	}

	result := wallCreateCommentResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return wallCreateCommentResponse{}, err
	}

	if err := result.Error.check(); err != nil {
		return wallCreateCommentResponse{}, err
	}

	return result.Response, nil
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func apiForm(fields map[string]string, files map[string]string) (io.Reader, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}

	for k, v := range files {
		field := strings.Split(k, ".")[0]
		fw, err := writer.CreateFormFile(field, k)

		if err != nil {
			return nil, "", err
		}

		if _, err := fw.Write([]byte(v)); err != nil {
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
	body, ct, err := apiForm(form, nil)

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
	ID      int           `json:"id"`
	Date    int           `json:"date"`
	Text    string        `json:"text"`
	Changes updateChanges `json:"changes"`
}

type updateChanges struct {
	Website updateValueChangeString `json:"website"`
}

type updateValueChangeString struct {
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
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
	body, ct, err := apiForm(form, nil)

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
	body, ct, err := apiForm(form, nil)

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

type docsGetWallUploadServerResult struct {
	Error    errorResult                     `json:"error"`
	Response docsGetWallUploadServerResponse `json:"response"`
}

type docsGetWallUploadServerResponse struct {
	UploadURL string `json:"upload_url"`
}

func docsGetWallUploadServer(cfg config) (docsGetWallUploadServerResponse, error) {
	values := apiValues(cfg)

	values.Set("group_id", cfg.API.ClubID)

	uri := apiURL(cfg, "docs.getWallUploadServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	data, err := apiDo(cfg, req)

	if err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	result := docsGetWallUploadServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	if err := result.Error.check(); err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	return result.Response, nil
}

type docsUploadParams struct {
	uploadURL string
	data      string
}

type docsUploadResult struct {
	Error      string `json:"error"`
	ErrorDescr string `json:"error_descr"`
	docsUploadResponse
}

type docsUploadResponse struct {
	File string `json:"file"`
}

func docsUpload(cfg config, params docsUploadParams) (docsUploadResponse, error) {
	files := map[string]string{
		"file.txt": params.data,
	}
	body, ct, err := apiForm(nil, files)

	if err != nil {
		return docsUploadResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, params.uploadURL, body)

	if err != nil {
		return docsUploadResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, req)

	if err != nil {
		return docsUploadResponse{}, err
	}

	result := docsUploadResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return docsUploadResponse{}, err
	}

	if len(result.Error) > 0 {
		return docsUploadResponse{}, errors.New(result.Error)
	}

	return result.docsUploadResponse, nil
}

type docsSaveParams struct {
	file string
}

type docsSaveResult struct {
	Error    errorResult      `json:"error"`
	Response docsSaveResponse `json:"response"`
}

type docsSaveResponse struct {
	Type string   `json:"type"`
	Doc  document `json:"doc"`
}

type document struct {
	ID   int    `json:"id"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

func docsSave(cfg config, params docsSaveParams) (docsSaveResponse, error) {
	values := apiValues(cfg)

	values.Set("file", params.file)

	uri := apiURL(cfg, "docs.save", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return docsSaveResponse{}, err
	}

	data, err := apiDo(cfg, req)

	if err != nil {
		return docsSaveResponse{}, err
	}

	result := docsSaveResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return docsSaveResponse{}, err
	}

	if err := result.Error.check(); err != nil {
		return docsSaveResponse{}, err
	}

	return result.Response, nil
}

type docsDownloadParams struct {
	url string
}

func docsDownload(cfg config, params docsDownloadParams) (string, error) {
	req, err := http.NewRequest(http.MethodGet, params.url, nil)

	if err != nil {
		return "", err
	}

	data, err := apiDo(cfg, req)

	if err != nil {
		return "", err
	}

	return string(data), nil
}

func docsUploadAndSave(cfg config, params docsUploadParams) (docsSaveResponse, error) {
	server, err := docsGetWallUploadServer(cfg)

	if err != nil {
		return docsSaveResponse{}, err
	}

	upload, err := docsUpload(cfg, docsUploadParams{
		uploadURL: server.UploadURL,
		data:      params.data,
	})

	if err != nil {
		return docsSaveResponse{}, err
	}

	saved, err := docsSave(cfg, docsSaveParams{
		file: upload.File,
	})

	if err != nil {
		return docsSaveResponse{}, err
	}

	return saved, nil
}

type groupsEditParams struct {
	website string
}

type groupsEditResult struct {
	Error    errorResult `json:"error"`
	Response int         `json:"response"`
}

func groupsEdit(cfg config, params groupsEditParams) (int, error) {
	values := apiValues(cfg)

	values.Set("group_id", cfg.API.ClubID)

	if len(params.website) > 0 {
		values.Set("website", params.website)
	}

	uri := apiURL(cfg, "groups.edit", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return 0, err
	}

	data, err := apiDo(cfg, req)

	if err != nil {
		return 0, err
	}

	result := groupsEditResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}

	if err := result.Error.check(); err != nil {
		return 0, err
	}

	return result.Response, nil
}

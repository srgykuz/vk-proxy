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

func apiURL(method string, values url.Values) string {
	method = strings.TrimPrefix(method, "/")

	return fmt.Sprintf("https://api.vk.ru/method/%v?%s", method, values.Encode())
}

func apiValues(token string) url.Values {
	return url.Values{
		"v":            []string{"5.199"},
		"access_token": []string{token},
	}
}

func apiForm(fields map[string]string, files map[string][]byte) (io.Reader, string, error) {
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

		if _, err := fw.Write(v); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return body, writer.FormDataContentType(), nil
}

func apiDo(cfg configAPI, club configClub, user configUser, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(req.Context(), cfg.Timeout())
	defer cancel()

	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)

	method := strings.TrimPrefix(req.URL.Path, "/method/")
	descr := fmt.Sprintf("(method=%v club=%v user=%v)", method, club.Name, user.Name)

	if err != nil {
		if e, ok := err.(*url.Error); ok {
			e.URL = req.URL.Path
		}

		return nil, fmt.Errorf("%v %v", err, descr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %v %v", resp.StatusCode, descr)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("read: %v %v", err, descr)
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		result := errorResult{}

		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("%v %v", err, descr)
		}

		if err := result.Error.check(); err != nil {
			return nil, fmt.Errorf("%v %v", err, descr)
		}
	}

	return data, nil
}

type apiDownloadParams struct {
	url string
}

func apiDownload(cfg configAPI, params apiDownloadParams) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, params.url, nil)

	if err != nil {
		return nil, err
	}

	data, err := apiDo(cfg, configClub{}, configUser{}, req)

	if err != nil {
		return nil, err
	}

	return data, nil
}

func apiDownloadURL(cfg configAPI, uri string) ([]byte, error) {
	p := apiDownloadParams{
		url: uri,
	}

	return apiDownload(cfg, p)
}

type errorResult struct {
	Error errorResponse `json:"error"`
}

type errorResponse struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (r errorResponse) check() error {
	if r.ErrorCode != 0 {
		return fmt.Errorf("code %d: %s", r.ErrorCode, r.ErrorMsg)
	}

	return nil
}

type messagesSendParams struct {
	message string
}

type messagesSendResult struct {
	Response int `json:"response"`
}

func messagesSend(cfg configAPI, club configClub, user configUser, params messagesSendParams) (int, error) {
	form := map[string]string{
		"user_id":   user.ID,
		"random_id": "0",
		"message":   params.message,
	}
	body, ct, err := apiForm(form, nil)

	if err != nil {
		return 0, err
	}

	values := apiValues(club.AccessToken)
	uri := apiURL("messages.send", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, club, user, req)

	if err != nil {
		return 0, err
	}

	result := messagesSendResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}

	return result.Response, nil
}

type groupsGetLongPollServerResult struct {
	Response groupsGetLongPollServerResponse `json:"response"`
}

type groupsGetLongPollServerResponse struct {
	Key    string      `json:"key"`
	Server string      `json:"server"`
	TS     json.Number `json:"ts"`
}

func groupsGetLongPollServer(cfg configAPI, club configClub) (groupsGetLongPollServerResponse, error) {
	values := apiValues(club.AccessToken)

	values.Set("group_id", club.ID)

	uri := apiURL("groups.getLongPollServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	data, err := apiDo(cfg, club, configUser{}, req)

	if err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	result := groupsGetLongPollServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return groupsGetLongPollServerResponse{}, err
	}

	return result.Response, nil
}

type groupsUseLongPollServerResponse struct {
	Failed  int         `json:"failed"`
	TS      json.Number `json:"ts"`
	Updates []update    `json:"updates"`
}

const (
	updateTypeMessageReply int = iota + 1
	updateTypeWallPostNew
	updateTypeWallReplyNew
	updateTypePhotoNew
)

type update struct {
	Type    string       `json:"type"`
	EventID string       `json:"event_id"`
	V       string       `json:"v"`
	GroupID int          `json:"group_id"`
	Object  updateObject `json:"object"`
}

func (u update) TypeEnum() int {
	switch u.Type {
	case "message_reply":
		return updateTypeMessageReply
	case "wall_post_new":
		return updateTypeWallPostNew
	case "wall_reply_new":
		return updateTypeWallReplyNew
	case "photo_new":
		return updateTypePhotoNew
	default:
		return 0
	}
}

type updateObject struct {
	ID        int         `json:"id"`
	Date      int         `json:"date"`
	Text      string      `json:"text"`
	OrigPhoto updatePhoto `json:"orig_photo"`
}

type updatePhoto struct {
	URL string `json:"url"`
}

func groupsUseLongPollServer(cfg configAPI, server groupsGetLongPollServerResponse, last groupsUseLongPollServerResponse) (groupsUseLongPollServerResponse, error) {
	values := url.Values{}

	values.Set("act", "a_check")
	values.Set("key", server.Key)
	values.Set("ts", last.TS.String())
	values.Set("wait", "25")

	cfg.TimeoutMS = 30 * 1000
	uri := fmt.Sprintf("%v?%v", server.Server, values.Encode())

	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return groupsUseLongPollServerResponse{}, err
	}

	data, err := apiDo(cfg, configClub{}, configUser{}, req)

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
	Response wallPostResponse `json:"response"`
}

type wallPostResponse struct {
	PostID int `json:"post_id"`
}

func wallPost(cfg configAPI, club configClub, params wallPostParams) (wallPostResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"message":  params.message,
	}
	body, ct, err := apiForm(form, nil)

	if err != nil {
		return wallPostResponse{}, err
	}

	values := apiValues(club.AccessToken)
	uri := apiURL("wall.post", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return wallPostResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, club, configUser{}, req)

	if err != nil {
		return wallPostResponse{}, err
	}

	result := wallPostResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return wallPostResponse{}, err
	}

	return result.Response, nil
}

type wallCreateCommentParams struct {
	postID  int
	message string
}

type wallCreateCommentResult struct {
	Response wallCreateCommentResponse `json:"response"`
}

type wallCreateCommentResponse struct {
	CommentID int `json:"comment_id"`
}

func wallCreateComment(cfg configAPI, club configClub, params wallCreateCommentParams) (wallCreateCommentResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"post_id":  fmt.Sprint(params.postID),
		"message":  params.message,
	}
	body, ct, err := apiForm(form, nil)

	if err != nil {
		return wallCreateCommentResponse{}, err
	}

	values := apiValues(club.AccessToken)
	uri := apiURL("wall.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return wallCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, club, configUser{}, req)

	if err != nil {
		return wallCreateCommentResponse{}, err
	}

	result := wallCreateCommentResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return wallCreateCommentResponse{}, err
	}

	return result.Response, nil
}

type docsGetWallUploadServerResult struct {
	Response docsGetWallUploadServerResponse `json:"response"`
}

type docsGetWallUploadServerResponse struct {
	UploadURL string `json:"upload_url"`
}

func docsGetWallUploadServer(cfg configAPI, club configClub) (docsGetWallUploadServerResponse, error) {
	values := apiValues(club.AccessToken)

	values.Set("group_id", club.ID)

	uri := apiURL("docs.getWallUploadServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	data, err := apiDo(cfg, club, configUser{}, req)

	if err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	result := docsGetWallUploadServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return docsGetWallUploadServerResponse{}, err
	}

	return result.Response, nil
}

type docsUploadParams struct {
	uploadURL string
	data      []byte
}

type docsUploadResult struct {
	Error      string `json:"error"`
	ErrorDescr string `json:"error_descr"`
	docsUploadResponse
}

type docsUploadResponse struct {
	File string `json:"file"`
}

func docsUpload(cfg configAPI, params docsUploadParams) (docsUploadResponse, error) {
	files := map[string][]byte{
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

	data, err := apiDo(cfg, configClub{}, configUser{}, req)

	if err != nil {
		return docsUploadResponse{}, err
	}

	result := docsUploadResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return docsUploadResponse{}, err
	}

	if len(result.Error) > 0 {
		return docsUploadResponse{}, fmt.Errorf("docs.upload: %v", result.Error)
	}

	return result.docsUploadResponse, nil
}

type docsSaveParams struct {
	file string
}

type docsSaveResult struct {
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

func docsSave(cfg configAPI, club configClub, params docsSaveParams) (docsSaveResponse, error) {
	values := apiValues(club.AccessToken)

	values.Set("file", params.file)

	uri := apiURL("docs.save", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return docsSaveResponse{}, err
	}

	data, err := apiDo(cfg, club, configUser{}, req)

	if err != nil {
		return docsSaveResponse{}, err
	}

	result := docsSaveResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return docsSaveResponse{}, err
	}

	return result.Response, nil
}

func docsUploadAndSave(cfg configAPI, club configClub, params docsUploadParams) (docsSaveResponse, error) {
	server, err := docsGetWallUploadServer(cfg, club)

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

	saved, err := docsSave(cfg, club, docsSaveParams{
		file: upload.File,
	})

	if err != nil {
		return docsSaveResponse{}, err
	}

	return saved, nil
}

type photosGetUploadServerResult struct {
	Response photosGetUploadServerResponse `json:"response"`
}

type photosGetUploadServerResponse struct {
	UploadURL string `json:"upload_url"`
}

func photosGetUploadServer(cfg configAPI, club configClub, user configUser) (photosGetUploadServerResponse, error) {
	values := apiValues(user.AccessToken)

	values.Set("group_id", club.ID)
	values.Set("album_id", club.AlbumID)

	uri := apiURL("photos.getUploadServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return photosGetUploadServerResponse{}, err
	}

	data, err := apiDo(cfg, club, user, req)

	if err != nil {
		return photosGetUploadServerResponse{}, err
	}

	result := photosGetUploadServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return photosGetUploadServerResponse{}, err
	}

	return result.Response, nil
}

type photosUploadParams struct {
	uploadURL string
	data      []byte
}

type photosUploadResult struct {
	Error      string `json:"error"`
	ErrorDescr string `json:"error_descr"`
	photosUploadResponse
}

type photosUploadResponse struct {
	Server     int    `json:"server"`
	PhotosList string `json:"photos_list"`
	Hash       string `json:"hash"`
}

func photosUpload(cfg configAPI, params photosUploadParams) (photosUploadResponse, error) {
	files := map[string][]byte{
		"file1.png": params.data,
	}
	body, ct, err := apiForm(nil, files)

	if err != nil {
		return photosUploadResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, params.uploadURL, body)

	if err != nil {
		return photosUploadResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := apiDo(cfg, configClub{}, configUser{}, req)

	if err != nil {
		return photosUploadResponse{}, err
	}

	result := photosUploadResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return photosUploadResponse{}, err
	}

	if len(result.Error) > 0 {
		return photosUploadResponse{}, fmt.Errorf("photos.upload: %v", result.Error)
	}

	if result.PhotosList == "" || result.PhotosList == "[]" {
		return photosUploadResponse{}, errors.New("photos.upload: not uploaded")
	}

	return result.photosUploadResponse, nil
}

type photosSaveParams struct {
	photosList string
	server     int
	hash       string
	caption    string
}

type photosSaveResult struct {
	Response []photosSaveResponse `json:"response"`
}

type photosSaveResponse struct {
	ID int `json:"id"`
}

func photosSave(cfg configAPI, club configClub, user configUser, params photosSaveParams) (photosSaveResponse, error) {
	values := apiValues(user.AccessToken)

	values.Set("group_id", club.ID)
	values.Set("album_id", club.AlbumID)
	values.Set("photos_list", params.photosList)
	values.Set("server", fmt.Sprint(params.server))
	values.Set("hash", params.hash)
	values.Set("caption", params.caption)

	uri := apiURL("photos.save", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return photosSaveResponse{}, err
	}

	data, err := apiDo(cfg, club, user, req)

	if err != nil {
		return photosSaveResponse{}, err
	}

	result := photosSaveResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return photosSaveResponse{}, err
	}

	if len(result.Response) == 0 {
		return photosSaveResponse{}, errors.New("photos.save: empty response")
	}

	return result.Response[0], nil
}

type photosUploadAndSaveParams struct {
	photosUploadParams
	photosSaveParams
}

func photosUploadAndSave(cfg configAPI, club configClub, user configUser, params photosUploadAndSaveParams) (photosSaveResponse, error) {
	server, err := photosGetUploadServer(cfg, club, user)

	if err != nil {
		return photosSaveResponse{}, err
	}

	params.photosUploadParams.uploadURL = server.UploadURL
	upload, err := photosUpload(cfg, params.photosUploadParams)

	if err != nil {
		return photosSaveResponse{}, err
	}

	params.photosSaveParams.photosList = upload.PhotosList
	params.photosSaveParams.server = upload.Server
	params.photosSaveParams.hash = upload.Hash
	saved, err := photosSave(cfg, club, user, params.photosSaveParams)

	if err != nil {
		return photosSaveResponse{}, err
	}

	return saved, nil
}

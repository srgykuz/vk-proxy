package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/skip2/go-qrcode"
)

func encodeQR(content string) ([]byte, error) {
	if len(content) > 2953 {
		return nil, errors.New("content is too large")
	}

	qr, err := qrcode.New(content, qrcode.Low)

	if err != nil {
		return nil, err
	}

	png, err := qr.PNG(256)

	if err != nil {
		return nil, err
	}

	return png, nil
}

func decodeQR(cfg config, file string) (string, error) {
	cmd := exec.Command(cfg.QR.ZBarPath, file)
	output, err := cmd.Output()

	if err != nil {
		return "", err
	}

	result, found := strings.CutPrefix(string(output), "QR-Code:")

	if !found {
		return "", errors.New("unexpected output")
	}

	result = strings.Trim(result, "\n")

	return result, nil
}

func saveQR(data []byte, ext string) (string, error) {
	pattern := "qr-*"

	if len(ext) > 0 {
		pattern += "." + ext
	}

	f, err := os.CreateTemp("", pattern)

	if err != nil {
		return "", err
	}

	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return "", err
	}

	if err := f.Sync(); err != nil {
		return "", err
	}

	return f.Name(), nil
}

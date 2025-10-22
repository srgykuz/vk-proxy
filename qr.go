package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/skip2/go-qrcode"
)

func encodeQR(content string) ([]byte, error) {
	if len(content) > 2953 {
		return nil, fmt.Errorf("too large content: %v", len(content))
	}

	qr, err := qrcode.New(content, qrcode.Low)

	if err != nil {
		return nil, err
	}

	png, err := qr.PNG(512)

	if err != nil {
		return nil, err
	}

	return png, nil
}

func decodeQR(cfg config, file string) (string, error) {
	buf := bytes.Buffer{}

	cmd := exec.Command(cfg.QR.ZBarPath, file)
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := buf.String()

	if err != nil {
		if strings.Contains(output, "scanned 0 barcode symbols") {
			return "", errors.New("qr code is not detected")
		} else if len(output) > 0 {
			return "", fmt.Errorf("%v: %v", err, output)
		}

		return "", err
	}

	lines := strings.Split(output, "\n")
	content := ""

	for _, line := range lines {
		s, found := strings.CutPrefix(line, "QR-Code:")

		if found {
			content = s
			break
		}
	}

	if len(content) == 0 {
		return "", fmt.Errorf("unexpected output: %v", output)
	}

	return content, nil
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

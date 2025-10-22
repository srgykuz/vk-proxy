package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/skip2/go-qrcode"
)

const qrSize = 512

func encodeQR(content string) ([]byte, error) {
	if len(content) > 2953 {
		return nil, fmt.Errorf("too large content: %v", len(content))
	}

	qr, err := qrcode.New(content, qrcode.Low)

	if err != nil {
		return nil, err
	}

	data, err := qr.PNG(qrSize)

	if err != nil {
		return nil, err
	}

	return data, nil
}

func decodeQR(cfg config, file string) ([]string, error) {
	buf := bytes.Buffer{}

	cmd := exec.Command(cfg.QR.ZBarPath, file)
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := buf.String()

	if err != nil {
		if strings.Contains(output, "scanned 0 barcode symbols") {
			return nil, errors.New("qr code is not detected")
		} else if len(output) > 0 {
			return nil, fmt.Errorf("%v: %v", err, output)
		}

		return nil, err
	}

	lines := strings.Split(output, "\n")
	content := []string{}

	for _, line := range lines {
		s, found := strings.CutPrefix(line, "QR-Code:")

		if found {
			content = append(content, s)
		}
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("unexpected output: %v", output)
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

func mergeQR(data [][]byte) ([]byte, error) {
	n := len(data)

	if n == 0 {
		return nil, fmt.Errorf("data is empty")
	}

	side := int(math.Ceil(math.Sqrt(float64(n))))
	cols := side
	rows := int(math.Ceil(float64(n) / float64(cols)))

	width := cols * qrSize
	height := rows * qrSize

	rect := image.Rect(0, 0, width, height)
	merged := image.NewNRGBA(rect)

	draw.Draw(merged, merged.Bounds(), image.White, image.Point{}, draw.Src)

	for i, b := range data {
		img, _, err := image.Decode(bytes.NewReader(b))

		if err != nil {
			return nil, fmt.Errorf("image decode: %v", err)
		}

		if img.Bounds().Dx() != qrSize || img.Bounds().Dy() != qrSize {
			return nil, fmt.Errorf("image size: %vx%v", img.Bounds().Dx(), img.Bounds().Dy())
		}

		rowN := i / cols
		colN := i % cols

		offsetX := colN * qrSize
		offsetY := rowN * qrSize

		point := image.Point{offsetX, offsetY}

		draw.Draw(merged, img.Bounds().Add(point), img, img.Bounds().Min, draw.Src)
	}

	var buf bytes.Buffer

	if err := png.Encode(&buf, merged); err != nil {
		return nil, fmt.Errorf("image encode: %v", err)
	}

	return buf.Bytes(), nil
}

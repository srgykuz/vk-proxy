package main

import (
	"bytes"
	"context"
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

type qrLevel = qrcode.RecoveryLevel

const (
	qrLevelLow     = qrcode.Low
	qrLevelMedium  = qrcode.Medium
	qrLevelHigh    = qrcode.High
	qrLevelHighest = qrcode.Highest
)

var qrMaxLen = map[qrLevel]int{
	qrLevelLow:     2953,
	qrLevelMedium:  2331,
	qrLevelHigh:    1663,
	qrLevelHighest: 1273,
}

func encodeQR(cfg configQR, content string) ([]byte, error) {
	level := qrLevel(cfg.ImageLevel)

	if len(content) > qrMaxLen[level] {
		return nil, fmt.Errorf("too large content: %v > %v", len(content), qrMaxLen[level])
	}

	qr, err := qrcode.New(content, level)

	if err != nil {
		return nil, err
	}

	data, err := qr.PNG(cfg.ImageSize)

	if err != nil {
		return nil, err
	}

	return data, nil
}

func decodeQR(cfg configQR, file string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ZBarTimeout())
	defer cancel()

	buf := bytes.Buffer{}
	cmd := exec.CommandContext(ctx, cfg.ZBarPath, file)

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

func saveQR(cfg configQR, data []byte, ext string) (string, error) {
	pattern := "qr-*"

	if len(ext) > 0 {
		pattern += "." + ext
	}

	f, err := os.CreateTemp(cfg.SaveDir, pattern)

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

func mergeQR(cfg configQR, data [][]byte) ([]byte, error) {
	n := len(data)

	if n == 0 {
		return nil, fmt.Errorf("data is empty")
	}

	side := int(math.Ceil(math.Sqrt(float64(n))))
	cols := side
	rows := int(math.Ceil(float64(n) / float64(cols)))

	size := cfg.ImageSize
	width := cols * size
	height := rows * size

	rect := image.Rect(0, 0, width, height)
	merged := image.NewNRGBA(rect)

	draw.Draw(merged, merged.Bounds(), image.White, image.Point{}, draw.Src)

	for i, b := range data {
		img, _, err := image.Decode(bytes.NewReader(b))

		if err != nil {
			return nil, fmt.Errorf("image decode: %v", err)
		}

		if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
			return nil, fmt.Errorf("image size: %vx%v", img.Bounds().Dx(), img.Bounds().Dy())
		}

		rowN := i / cols
		colN := i % cols

		offsetX := colN * size
		offsetY := rowN * size

		point := image.Point{offsetX, offsetY}

		draw.Draw(merged, img.Bounds().Add(point), img, img.Bounds().Min, draw.Src)
	}

	var buf bytes.Buffer

	if err := png.Encode(&buf, merged); err != nil {
		return nil, fmt.Errorf("image encode: %v", err)
	}

	return buf.Bytes(), nil
}

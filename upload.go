package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"

	_ "golang.org/x/image/webp" // registers the WebP decoder
)

const (
	maxScreenshots     = 3
	maxScreenshotBytes = 5 << 20
	maxPixels          = 40_000_000
)

var screenshotName = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg)$`)

var errBadImage = errors.New("Screenshots must be PNG, JPEG or WebP images under 5 MB.")

// saveScreenshot decodes and re-encodes the upload, which drops EXIF data
// (GPS location, device model) that could identify an anonymous submitter.
func saveScreenshot(dir string, fh *multipart.FileHeader) (string, error) {
	if fh.Size > maxScreenshotBytes {
		return "", errBadImage
	}
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxScreenshotBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxScreenshotBytes {
		return "", errBadImage
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
		return "", errBadImage
	}
	if cfg.Width*cfg.Height > maxPixels {
		return "", errBadImage
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", errBadImage
	}

	id := make([]byte, 16)
	rand.Read(id)
	name := hex.EncodeToString(id) + ".png"
	if format == "jpeg" {
		name = hex.EncodeToString(id) + ".jpg"
	}

	var out bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&out, img)
	}
	if err != nil {
		return "", err
	}
	return name, os.WriteFile(filepath.Join(dir, name), out.Bytes(), 0o644)
}

func removeScreenshots(dir string, names []string) {
	for _, n := range names {
		os.Remove(filepath.Join(dir, n))
	}
}

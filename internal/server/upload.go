package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

// Screenshots are stored in Postgres rather than on disk, so the app runs on hosts without a
// persistent disk and backups include them.

const (
	maxScreenshots     = 3
	maxScreenshotBytes = 5 << 20
	maxPixels          = 40_000_000
	maxScreenshotWidth = 1400 // wider images are scaled down to save space and visitors' data
)

var screenshotName = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg)$`)

var errBadImage = errors.New("Screenshots must be PNG, JPEG or WebP images under 5 MB.")

// saveScreenshot decodes and re-encodes the upload, which drops EXIF data (GPS location,
// device model) that could identify an anonymous submitter, scales it down if it's very
// wide, and stores it. It returns the screenshot's name.
func (a *app) saveScreenshot(ctx context.Context, fh *multipart.FileHeader) (string, error) {
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
	if b := img.Bounds(); b.Dx() > maxScreenshotWidth {
		h := b.Dy() * maxScreenshotWidth / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, maxScreenshotWidth, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}

	id := make([]byte, 16)
	rand.Read(id)
	var out bytes.Buffer
	name, contentType := hex.EncodeToString(id)+".png", "image/png"
	if format == "jpeg" {
		name, contentType = hex.EncodeToString(id)+".jpg", "image/jpeg"
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&out, img)
	}
	if err != nil {
		return "", err
	}
	_, err = a.db.Exec(ctx, `INSERT INTO screenshots (name, content_type, data) VALUES ($1, $2, $3)`,
		name, contentType, out.Bytes())
	return name, err
}

func (a *app) removeScreenshots(ctx context.Context, names []string) {
	if len(names) > 0 {
		a.db.Exec(ctx, `DELETE FROM screenshots WHERE name = ANY ($1)`, names)
	}
}

// writeScreenshot serves a stored screenshot. publicOnly hides ones whose issue was removed.
func (a *app) writeScreenshot(w http.ResponseWriter, r *http.Request, publicOnly bool) {
	name := r.PathValue("name")
	if !screenshotName.MatchString(name) {
		a.notFound(w, r)
		return
	}
	var contentType string
	var data []byte
	var created time.Time
	err := a.db.QueryRow(r.Context(), `
		SELECT s.content_type, s.data, s.created_at FROM screenshots s
		WHERE s.name = $1 AND (NOT $2 OR EXISTS (
			SELECT 1 FROM issues i WHERE s.name = ANY (i.screenshots) AND i.status <> 'removed'))`,
		name, publicOnly).Scan(&contentType, &data, &created)
	if err != nil {
		a.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if publicOnly {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeContent(w, r, name, created, bytes.NewReader(data))
}

package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestPostgresUploadAndSignedReadPreserveImage(t *testing.T) {
	s := testServer(t, config.Personal)
	token := initialize(t, s)
	img := image.NewRGBA(image.Rect(0, 0, 30, 50))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(29, 49, color.RGBA{B: 255, A: 255})
	var raw bytes.Buffer
	_ = png.Encode(&raw, img)
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, err := mw.CreateFormFile("file", "../../test.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(raw.Bytes())
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/assets", &form)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("upload failed", w.Code)
	}
	var reply struct {
		Data struct {
			ID       string `json:"id"`
			URL      string `json:"url"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Filename string `json:"original_filename"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Data.Width != 30 || reply.Data.Height != 50 || strings.Contains(reply.Data.Filename, "..") {
		t.Fatal("image geometry or filename incorrect")
	}
	u, err := url.Parse(reply.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", u.RequestURI(), nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("signed file read failed")
	}
	decoded, _, err := image.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || decoded.Bounds().Dx() != 30 || decoded.Bounds().Dy() != 50 {
		t.Fatal("download distorted")
	}
	q := u.Query()
	q.Set("signature", "invalid")
	u.RawQuery = q.Encode()
	r = httptest.NewRequest("GET", u.RequestURI(), nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("tampered signed URL accepted")
	}
}

func TestPostgresUploadCannotAttachToAnotherProject(t *testing.T) {
	s := testServer(t, config.Personal)
	token := initialize(t, s)
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, _ := mw.CreateFormFile("file", "test.png")
	_ = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/projects/unowned/product-images", &form)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("upload allowed foreign project")
	}
}

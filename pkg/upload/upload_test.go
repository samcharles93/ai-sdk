package upload

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDetectMediaType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if mt := DetectMediaType(png); mt != "image/png" {
		t.Fatalf("expected image/png, got %q", mt)
	}
	jpg := []byte{0xff, 0xd8, 0xff, 0x00}
	if mt := DetectMediaType(jpg); mt != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", mt)
	}
	gif := []byte{'G', 'I', 'F', '8', '9', 'a'}
	if mt := DetectMediaType(gif); mt != "image/gif" {
		t.Fatalf("expected image/gif, got %q", mt)
	}
	pdf := []byte{'%', 'P', 'D', 'F', '-'}
	if mt := DetectMediaType(pdf); mt != "application/pdf" {
		t.Fatalf("expected application/pdf, got %q", mt)
	}
}

func TestToBase64(t *testing.T) {
	f := File{Data: []byte("hello")}
	got := ToBase64(f)
	want := base64.StdEncoding.EncodeToString([]byte("hello"))
	if got != want {
		t.Fatalf("base64 mismatch: want %q got %q", want, got)
	}
}

func TestParseMultipartForm(t *testing.T) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("abc123"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())

	files, err := ParseMultipartForm(req, 10<<20)
	if err != nil {
		t.Fatalf("parse multipart: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Name != "test.txt" {
		t.Fatalf("expected filename test.txt got %q", files[0].Name)
	}
	if string(files[0].Data) != "abc123" {
		t.Fatalf("content mismatch")
	}
}

func TestParseSkill(t *testing.T) {
	js := []byte(`{"name":"s1","description":"d","template":"tpl"}`)
	s, err := ParseSkill(js)
	if err != nil {
		t.Fatalf("parse skill: %v", err)
	}
	if s.Name != "s1" || s.Description != "d" || s.Template != "tpl" {
		t.Fatalf("unexpected skill: %+v", s)
	}
}

package upload

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
)

// File represents an uploaded file.
type File struct {
	Name      string
	Data      []byte
	MediaType string
	Size      int64
}

// ParseMultipartForm parses a multipart form from the provided request.
// maxMemory is passed to r.ParseMultipartForm and controls how much data
// is kept in memory before being written to temporary files.
func ParseMultipartForm(r *http.Request, maxMemory int64) ([]File, error) {
	if r == nil {
		return nil, errors.New("nil request")
	}
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		return nil, err
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	var out []File
	if r.MultipartForm == nil {
		return out, nil
	}
	for _, fhs := range r.MultipartForm.File {
		for _, fh := range fhs {
			rc, err := fh.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			mt := DetectMediaType(data)
			out = append(out, File{
				Name:      fh.Filename,
				Data:      data,
				MediaType: mt,
				Size:      int64(len(data)),
			})
		}
	}
	return out, nil
}

// ToBase64 returns the base64 encoding of the file's data.
func ToBase64(f File) string {
	return base64.StdEncoding.EncodeToString(f.Data)
}

// DetectMediaType attempts to identify common media types by inspecting
// the initial bytes of the provided data. Returns an empty string when
// the type is unknown.
func DetectMediaType(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte("GIF8")) {
		return "image/gif"
	}
	if len(data) >= 4 && data[0] == '%' && data[1] == 'P' && data[2] == 'D' && data[3] == 'F' {
		return "application/pdf"
	}
	// Fallback to empty string
	return ""
}

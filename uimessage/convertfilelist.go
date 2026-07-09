package uimessage

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
)

// ConvertMultipartFiles reads each file in a multipart form file slice
// and returns FileUIParts with data: URLs ("data:<mediaType>;base64,...").
func ConvertMultipartFiles(files []*multipart.FileHeader) ([]FileUIPart, error) {
	out := make([]FileUIPart, 0, len(files))
	for _, fh := range files {
		p, err := convertOne(fh)
		if err != nil {
			return nil, fmt.Errorf("convert %q: %w", fh.Filename, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func convertOne(fh *multipart.FileHeader) (FileUIPart, error) {
	mediaType := fh.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if i := strings.Index(mediaType, ";"); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	f, err := fh.Open()
	if err != nil {
		return FileUIPart{}, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return FileUIPart{}, err
	}
	url := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
	return FileUIPart{
		MediaType: mediaType,
		Filename:  fh.Filename,
		URL:       url,
	}, nil
}

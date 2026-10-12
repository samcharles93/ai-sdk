# upload

```go
ParseMultipartForm(r *http.Request, maxMemory int64) ([]File, error)
DetectMediaType(data []byte) string  // PNG, JPEG, GIF, PDF detection
ToBase64(f File) string
```

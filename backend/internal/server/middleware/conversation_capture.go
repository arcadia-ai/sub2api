package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type limitedCaptureWriter struct {
	gin.ResponseWriter
	buffer    bytes.Buffer
	limit     int
	total     int
	truncated bool
}

type limitedCaptureReader struct {
	io.ReadCloser
	buffer    bytes.Buffer
	limit     int
	total     int
	truncated bool
}

func (r *limitedCaptureReader) Read(data []byte) (int, error) {
	n, err := r.ReadCloser.Read(data)
	if n > 0 {
		r.total += n
		remaining := r.limit - r.buffer.Len()
		if remaining <= 0 {
			r.truncated = true
		} else if n > remaining {
			_, _ = r.buffer.Write(data[:remaining])
			r.truncated = true
		} else {
			_, _ = r.buffer.Write(data[:n])
		}
	}
	return n, err
}

func (w *limitedCaptureWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *limitedCaptureWriter) WriteString(data string) (int, error) {
	w.capture([]byte(data))
	return w.ResponseWriter.WriteString(data)
}

func (w *limitedCaptureWriter) capture(data []byte) {
	w.total += len(data)
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = true
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
		w.truncated = true
	}
	_, _ = w.buffer.Write(data)
}

func ConversationCapture(conversations *service.ConversationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if conversations == nil || !conversations.Enabled() || !isConversationEndpoint(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		apiKey, ok := GetAPIKeyFromContext(c)
		if !ok || apiKey == nil {
			c.Next()
			return
		}

		started := time.Now()
		requestReader := &limitedCaptureReader{ReadCloser: c.Request.Body, limit: conversations.MaxRequestBytes()}
		c.Request.Body = requestReader

		writer := &limitedCaptureWriter{ResponseWriter: c.Writer, limit: conversations.MaxResponseBytes()}
		c.Writer = writer
		c.Next()

		status := writer.Status()
		captureStatus := "success"
		if status >= http.StatusBadRequest {
			captureStatus = "failed"
		}
		provider := ""
		if apiKey.Group != nil {
			provider = apiKey.Group.Platform
		}
		conversations.Submit(&service.ConversationCapture{
			RequestUUID:       uuid.NewString(),
			UserID:            apiKey.UserID,
			APIKeyID:          apiKey.ID,
			Provider:          provider,
			Endpoint:          c.Request.URL.Path,
			Status:            captureStatus,
			HTTPStatus:        status,
			StartedAt:         started,
			CompletedAt:       time.Now(),
			DurationMS:        time.Since(started).Milliseconds(),
			RawRequestBytes:   int64(requestReader.total),
			RawResponseBytes:  int64(writer.total),
			RequestTruncated:  requestReader.truncated,
			ResponseTruncated: writer.truncated,
			RawRequest:        append([]byte(nil), requestReader.buffer.Bytes()...),
			RawResponse:       append([]byte(nil), writer.buffer.Bytes()...),
		})
	}
}

func isConversationEndpoint(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses":
		return true
	}
	return strings.HasPrefix(path, "/v1beta/models/") &&
		(strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent"))
}

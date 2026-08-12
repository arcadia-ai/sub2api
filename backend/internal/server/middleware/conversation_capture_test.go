package middleware

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitedCaptureReaderKeepsForwardBodyComplete(t *testing.T) {
	original := []byte("0123456789")
	req, err := http.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(original))
	require.NoError(t, err)

	reader := &limitedCaptureReader{ReadCloser: req.Body, limit: 4}
	forward, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, original, forward)
	require.Equal(t, []byte("0123"), reader.buffer.Bytes())
	require.Equal(t, len(original), reader.total)
	require.True(t, reader.truncated)
}

func TestConversationEndpoints(t *testing.T) {
	require.True(t, isConversationEndpoint(http.MethodPost, "/v1/messages"))
	require.True(t, isConversationEndpoint(http.MethodPost, "/v1beta/models/gemini-2.5:streamGenerateContent"))
	require.False(t, isConversationEndpoint(http.MethodPost, "/v1/images/generations"))
	require.False(t, isConversationEndpoint(http.MethodGet, "/v1/responses"))
}

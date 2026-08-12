package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConversationRequestAndHistoryHash(t *testing.T) {
	first := parseConversationRequest([]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`))
	require.Equal(t, "gpt-test", first.model)
	require.Empty(t, first.historyHash)

	second := parseConversationRequest([]byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"world"},{"role":"user","content":"again"}]}`))
	require.Equal(t, hashMessages(second.messages[:2]), second.historyHash)
	require.Equal(t, "hello", second.title)
}

func TestParseConversationResponseProtocols(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"openai", `{"choices":[{"message":{"role":"assistant","content":"openai"}}]}`, "openai"},
		{"claude stream", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"claude\"}}\n\n", "claude"},
		{"responses stream", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"responses\"}\n\n", "responses"},
		{"gemini", `{"candidates":[{"content":{"role":"model","parts":[{"text":"gemini"}]}}]}`, "gemini"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, _, _ := parseConversationResponse([]byte(tt.body))
			require.Len(t, messages, 1)
			require.Equal(t, "assistant", messages[0].Role)
			require.Equal(t, tt.want, messages[0].Text)
		})
	}
}

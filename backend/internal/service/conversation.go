package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/klauspost/compress/zstd"
	"go.uber.org/zap"
)

type ConversationMessage struct {
	Role string `json:"role"`
	Text string `json:"text,omitempty"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}
type ConversationCapture struct {
	RequestUUID                                string
	UserID, APIKeyID                           int64
	Provider, Endpoint, RequestedModel, Status string
	Stream                                     bool
	HTTPStatus                                 int
	HistoryHash, ResultHash, Title             string
	InputTokens, OutputTokens, DurationMS      int64
	RawRequestBytes, RawResponseBytes          int64
	RequestTruncated, ResponseTruncated        bool
	StartedAt, CompletedAt                     time.Time
	RawRequest, RawResponse                    []byte
	NormalizedRequest, NormalizedResponse      []ConversationMessage
}
type ConversationFilter struct {
	Page, PageSize       int
	UserID, APIKeyID     int64
	Model, Status, Query string
	StartTime, EndTime   *time.Time
}
type ConversationSession struct {
	ID                int64     `json:"id"`
	SessionUUID       string    `json:"session_uuid"`
	UserID            int64     `json:"user_id"`
	UserEmail         string    `json:"user_email"`
	APIKeyID          *int64    `json:"api_key_id,omitempty"`
	APIKeyName        string    `json:"api_key_name"`
	Title             string    `json:"title"`
	FirstModel        string    `json:"first_model"`
	LastModel         string    `json:"last_model"`
	MergeSource       string    `json:"merge_source"`
	RequestCount      int       `json:"request_count"`
	TotalInputTokens  int64     `json:"total_input_tokens"`
	TotalOutputTokens int64     `json:"total_output_tokens"`
	FirstRequestAt    time.Time `json:"first_request_at"`
	LastRequestAt     time.Time `json:"last_request_at"`
	LastStatus        string    `json:"last_status"`
}
type ConversationRequest struct {
	ID                int64                 `json:"id"`
	RequestUUID       string                `json:"request_uuid"`
	SessionID         int64                 `json:"session_id"`
	ParentRequestID   *int64                `json:"parent_request_id,omitempty"`
	Provider          string                `json:"provider"`
	Endpoint          string                `json:"endpoint"`
	RequestedModel    string                `json:"requested_model"`
	Stream            bool                  `json:"stream"`
	Status            string                `json:"status"`
	HTTPStatus        int                   `json:"http_status"`
	InputTokens       int64                 `json:"input_tokens"`
	OutputTokens      int64                 `json:"output_tokens"`
	DurationMS        int64                 `json:"duration_ms"`
	RequestTruncated  bool                  `json:"request_truncated"`
	ResponseTruncated bool                  `json:"response_truncated"`
	StartedAt         time.Time             `json:"started_at"`
	CompletedAt       time.Time             `json:"completed_at"`
	Messages          []ConversationMessage `json:"messages"`
}
type ConversationRawPayload struct {
	RequestID                    int64
	ContentType, ContentEncoding string
	Content                      []byte
}
type ConversationRepository interface {
	Save(context.Context, *ConversationCapture, []byte, []byte) error
	List(context.Context, *ConversationFilter) ([]ConversationSession, int64, error)
	Get(context.Context, int64) (*ConversationSession, []ConversationRequest, error)
	GetRaw(context.Context, int64, bool) (*ConversationRawPayload, error)
	DeleteSession(context.Context, int64) error
	ApplyRetention(context.Context, time.Time, time.Time, time.Time, int) (int64, error)
}

type ConversationService struct {
	repo     ConversationRepository
	cfg      config.ConversationStorageConfig
	queue    chan *ConversationCapture
	stop     chan struct{}
	wg       sync.WaitGroup
	decoder  *zstd.Decoder
	decodeMu sync.Mutex
}

func NewConversationService(repo ConversationRepository, cfg *config.Config) *ConversationService {
	dec, _ := zstd.NewReader(nil)
	s := &ConversationService{repo: repo, cfg: cfg.ConversationStorage, stop: make(chan struct{}), decoder: dec}
	if !s.cfg.Enabled {
		return s
	}
	if s.cfg.QueueSize < 1 {
		s.cfg.QueueSize = 2000
	}
	if s.cfg.WorkerCount < 1 {
		s.cfg.WorkerCount = 1
	}
	if s.cfg.RawSuccessDays < 1 {
		s.cfg.RawSuccessDays = 400
	}
	if s.cfg.RawFailedDays < 1 {
		s.cfg.RawFailedDays = 180
	}
	if s.cfg.NormalizedTextDays < 1 {
		s.cfg.NormalizedTextDays = 730
	}
	s.queue = make(chan *ConversationCapture, s.cfg.QueueSize)
	for i := 0; i < s.cfg.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	s.wg.Add(1)
	go s.cleanupLoop()
	return s
}
func (s *ConversationService) Enabled() bool { return s != nil && s.cfg.Enabled && s.repo != nil }
func (s *ConversationService) MaxRequestBytes() int {
	if s == nil || s.cfg.MaxRequestBytes < 1 {
		return 32 << 20
	}
	return s.cfg.MaxRequestBytes
}
func (s *ConversationService) MaxResponseBytes() int {
	if s == nil || s.cfg.MaxResponseBytes < 1 {
		return 32 << 20
	}
	return s.cfg.MaxResponseBytes
}
func (s *ConversationService) Submit(x *ConversationCapture) {
	if !s.Enabled() || x == nil {
		return
	}
	p := parseConversationRequest(x.RawRequest)
	x.RequestedModel = p.model
	if x.RequestedModel == "" {
		x.RequestedModel = modelFromEndpoint(x.Endpoint)
	}
	x.Stream = p.stream
	x.NormalizedRequest = p.messages
	x.HistoryHash = p.historyHash
	x.Title = p.title
	x.NormalizedResponse, x.InputTokens, x.OutputTokens = parseConversationResponse(x.RawResponse)
	x.ResultHash = hashMessages(append(append([]ConversationMessage{}, x.NormalizedRequest...), x.NormalizedResponse...))
	select {
	case s.queue <- x:
	default:
		logger.L().Warn("conversation.capture_queue_full", zap.Int64("user_id", x.UserID), zap.String("endpoint", x.Endpoint))
	}
}
func (s *ConversationService) worker() {
	defer s.wg.Done()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		logger.L().Error("conversation.encoder_init_failed", zap.Error(err))
		return
	}
	defer func() { _ = encoder.Close() }()
	for {
		select {
		case x := <-s.queue:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := s.repo.Save(ctx, x, encoder.EncodeAll(x.RawRequest, nil), encoder.EncodeAll(x.RawResponse, nil))
			cancel()
			if err != nil {
				logger.L().Error("conversation.save_failed", zap.String("request_uuid", x.RequestUUID), zap.Error(err))
			}
		case <-s.stop:
			return
		}
	}
}
func (s *ConversationService) cleanupLoop() {
	defer s.wg.Done()
	d := time.Duration(s.cfg.CleanupIntervalMinutes) * time.Minute
	if d <= 0 {
		d = time.Hour
	}
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.cleanup()
		case <-s.stop:
			return
		}
	}
}
func (s *ConversationService) cleanup() {
	now := time.Now()
	limit := s.cfg.CleanupBatchSize
	if limit < 1 {
		limit = 500
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for i := 0; i < 20; i++ {
		affected, err := s.repo.ApplyRetention(ctx, now.AddDate(0, 0, -s.cfg.RawSuccessDays), now.AddDate(0, 0, -s.cfg.RawFailedDays), now.AddDate(0, 0, -s.cfg.NormalizedTextDays), limit)
		if err != nil {
			logger.L().Error("conversation.retention_failed", zap.Error(err))
			return
		}
		if affected < int64(limit) {
			return
		}
	}
}
func (s *ConversationService) Stop() {
	if s == nil || s.queue == nil {
		return
	}
	close(s.stop)
	s.wg.Wait()
	s.decoder.Close()
}
func (s *ConversationService) List(c context.Context, f *ConversationFilter) ([]ConversationSession, int64, error) {
	return s.repo.List(c, f)
}
func (s *ConversationService) Get(c context.Context, id int64) (*ConversationSession, []ConversationRequest, error) {
	return s.repo.Get(c, id)
}
func (s *ConversationService) Delete(c context.Context, id int64) error {
	return s.repo.DeleteSession(c, id)
}
func (s *ConversationService) GetRaw(c context.Context, id int64, response bool) (*ConversationRawPayload, error) {
	p, e := s.repo.GetRaw(c, id, response)
	if e != nil {
		return nil, e
	}
	s.decodeMu.Lock()
	p.Content, e = s.decoder.DecodeAll(p.Content, nil)
	s.decodeMu.Unlock()
	if e != nil {
		return nil, errors.New("decompress conversation payload: " + e.Error())
	}
	p.ContentEncoding = "identity"
	return p, nil
}

func modelFromEndpoint(endpoint string) string {
	const prefix = "/v1beta/models/"
	if !strings.HasPrefix(endpoint, prefix) {
		return ""
	}
	model := strings.TrimPrefix(endpoint, prefix)
	if index := strings.IndexByte(model, ':'); index >= 0 {
		model = model[:index]
	}
	return model
}

type parsedConversationRequest struct {
	model              string
	stream             bool
	messages           []ConversationMessage
	historyHash, title string
}

func parseConversationRequest(body []byte) parsedConversationRequest {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return parsedConversationRequest{}
	}
	p := parsedConversationRequest{model: str(root["model"]), stream: boolean(root["stream"])}
	if v := text(root["system"]); v != "" {
		p.messages = append(p.messages, ConversationMessage{Role: "system", Text: v})
	}
	if v := text(root["instructions"]); v != "" {
		p.messages = append(p.messages, ConversationMessage{Role: "system", Text: v})
	}
	for _, k := range []string{"messages", "input", "contents"} {
		if root[k] != nil {
			p.messages = append(p.messages, normalizeMessages(root[k])...)
			break
		}
	}
	if len(p.messages) > 1 {
		p.historyHash = hashMessages(p.messages[:len(p.messages)-1])
	}
	for _, m := range p.messages {
		if m.Role == "user" && m.Text != "" {
			p.title = truncateConversationTitle(m.Text, 120)
			break
		}
	}
	return p
}
func parseConversationResponse(body []byte) ([]ConversationMessage, int64, int64) {
	var out []ConversationMessage
	var inTok, outTok int64
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		return out, 0, 0
	}
	if trim[0] == '{' || trim[0] == '[' {
		parseResponseJSON(trim, &out, &inTok, &outTok)
	} else {
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(make([]byte, 4096), 64<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if !bytes.Equal(data, []byte("[DONE]")) {
				parseResponseJSON(data, &out, &inTok, &outTok)
			}
		}
	}
	return compact(out), inTok, outTok
}
func parseResponseJSON(data []byte, out *[]ConversationMessage, inTok, outTok *int64) {
	var r map[string]any
	if json.Unmarshal(data, &r) != nil {
		return
	}
	usage := func(v any) {
		if u, ok := v.(map[string]any); ok {
			maxv(inTok, num(u["input_tokens"]))
			maxv(inTok, num(u["prompt_tokens"]))
			maxv(inTok, num(u["promptTokenCount"]))
			maxv(outTok, num(u["output_tokens"]))
			maxv(outTok, num(u["completion_tokens"]))
			maxv(outTok, num(u["candidatesTokenCount"]))
		}
	}
	usage(r["usage"])
	usage(r["usageMetadata"])
	if m, ok := r["message"].(map[string]any); ok {
		*out = append(*out, normalizeMessage(m, "assistant")...)
	}
	if v := text(r["content"]); v != "" {
		*out = append(*out, ConversationMessage{Role: "assistant", Text: v})
	}
	if r["output"] != nil {
		*out = append(*out, normalizeMessages(r["output"])...)
	}
	if cs, ok := r["choices"].([]any); ok {
		for _, v := range cs {
			if c, ok := v.(map[string]any); ok {
				for _, k := range []string{"message", "delta"} {
					if m, ok := c[k].(map[string]any); ok {
						*out = append(*out, normalizeMessage(m, "assistant")...)
					}
				}
			}
		}
	}
	if d, ok := r["delta"].(map[string]any); ok {
		if v := text(d["text"]); v != "" {
			*out = append(*out, ConversationMessage{Role: "assistant", Text: v})
		}
	} else if v := text(r["delta"]); v != "" {
		typ := ""
		if strings.Contains(str(r["type"]), "reasoning") {
			typ = "thinking"
		}
		*out = append(*out, ConversationMessage{Role: "assistant", Text: v, Type: typ})
	}
	if resp, ok := r["response"].(map[string]any); ok {
		usage(resp["usage"])
		// Responses streaming emits a complete response after the text deltas.
		// Keep the final object only when no delta text has already been collected.
		if resp["output"] != nil && len(*out) == 0 {
			*out = append(*out, normalizeMessages(resp["output"])...)
		}
	}
	if cs, ok := r["candidates"].([]any); ok {
		for _, v := range cs {
			if c, ok := v.(map[string]any); ok {
				*out = append(*out, normalizeMessages(c["content"])...)
			}
		}
	}
}
func normalizeMessages(v any) []ConversationMessage {
	var out []ConversationMessage
	switch x := v.(type) {
	case string:
		out = append(out, ConversationMessage{Role: "user", Text: x})
	case map[string]any:
		out = append(out, normalizeMessage(x, "user")...)
	case []any:
		for _, entry := range x {
			if message, ok := entry.(map[string]any); ok {
				out = append(out, normalizeMessage(message, "user")...)
			}
		}
	}
	return out
}

func normalizeMessage(message map[string]any, fallbackRole string) []ConversationMessage {
	role := str(message["role"])
	if role == "model" {
		role = "assistant"
	}
	if role == "" {
		role = fallbackRole
	}
	var value string
	for _, key := range []string{"content", "parts", "text", "output_text"} {
		if value = text(message[key]); value != "" {
			break
		}
	}
	if value == "" {
		return nil
	}
	return []ConversationMessage{{Role: role, Text: value, Type: str(message["type"]), Name: str(message["name"])}}
}

func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		for _, key := range []string{"text", "output_text", "content", "value", "arguments"} {
			if value := text(x[key]); value != "" {
				return value
			}
		}
	case []any:
		var b strings.Builder
		for _, entry := range x {
			_, _ = b.WriteString(text(entry))
		}
		return b.String()
	}
	return ""
}

func compact(in []ConversationMessage) []ConversationMessage {
	var out []ConversationMessage
	for _, message := range in {
		if message.Text == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == message.Role && out[len(out)-1].Type == message.Type {
			out[len(out)-1].Text += message.Text
		} else {
			out = append(out, message)
		}
	}
	return out
}
func hashMessages(messages []ConversationMessage) string {
	if len(messages) == 0 {
		return ""
	}
	b, _ := json.Marshal(messages)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func str(v any) string   { s, _ := v.(string); return s }
func boolean(v any) bool { b, _ := v.(bool); return b }
func num(v any) int64    { n, _ := v.(float64); return int64(n) }
func maxv(target *int64, value int64) {
	if value > *target {
		*target = value
	}
}
func truncateConversationTitle(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return string(runes)
}

package admin

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ConversationHandler struct {
	service *service.ConversationService
}

func NewConversationHandler(conversationService *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{service: conversationService}
}

// List returns conversation metadata only; raw payloads are fetched explicitly.
func (h *ConversationHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > 200 {
		pageSize = 200
	}
	filter := &service.ConversationFilter{
		Page:     page,
		PageSize: pageSize,
		Model:    strings.TrimSpace(c.Query("model")),
		Status:   strings.TrimSpace(c.Query("status")),
		Query:    strings.TrimSpace(c.Query("q")),
	}
	if !parsePositiveID(c, "user_id", &filter.UserID) || !parsePositiveID(c, "api_key_id", &filter.APIKeyID) {
		return
	}
	if !parseRFC3339(c, "start_time", &filter.StartTime) || !parseRFC3339(c, "end_time", &filter.EndTime) {
		return
	}
	items, total, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *ConversationHandler) Get(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	session, requests, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		writeConversationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"session": session, "requests": requests})
}

func (h *ConversationHandler) Delete(c *gin.Context) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		writeConversationError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *ConversationHandler) RawRequest(c *gin.Context)  { h.raw(c, false) }
func (h *ConversationHandler) RawResponse(c *gin.Context) { h.raw(c, true) }

func (h *ConversationHandler) raw(c *gin.Context, responsePayload bool) {
	id, ok := parsePathID(c)
	if !ok {
		return
	}
	payload, err := h.service.GetRaw(c.Request.Context(), id, responsePayload)
	if err != nil {
		writeConversationError(c, err)
		return
	}
	kind := "request"
	if responsePayload {
		kind = "response"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="conversation-%d-%s.bin"`, id, kind))
	c.Data(http.StatusOK, payload.ContentType, payload.Content)
}

func parsePathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid id")
		return 0, false
	}
	return id, true
}

func parsePositiveID(c *gin.Context, name string, target *int64) bool {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return true
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return false
	}
	*target = id
	return true
}

func parseRFC3339(c *gin.Context, name string, target **time.Time) bool {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		response.BadRequest(c, "Invalid "+name+", expect RFC3339")
		return false
	}
	*target = &parsed
	return true
}

func writeConversationError(c *gin.Context, err error) {
	if err == sql.ErrNoRows {
		response.NotFound(c, "Conversation record not found")
		return
	}
	response.ErrorFrom(c, err)
}

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type ConversationRepository struct{ db *sql.DB }

func NewConversationRepository(db *sql.DB) service.ConversationRepository {
	return &ConversationRepository{db: db}
}

func (r *ConversationRepository) Save(ctx context.Context, item *service.ConversationCapture, rawRequest, rawResponse []byte) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var sessionID int64
	var parentID sql.NullInt64
	mergeSource := "isolated"
	if item.HistoryHash != "" {
		err = tx.QueryRowContext(ctx, `WITH matches AS (
			SELECT r.session_id,r.id,r.completed_at FROM conversation_requests r
			JOIN conversation_sessions s ON s.id=r.session_id
			WHERE r.user_id=$1 AND r.api_key_id=$2 AND r.result_hash=$3 AND s.deleted_at IS NULL)
			SELECT session_id,id FROM matches
			WHERE (SELECT COUNT(*) FROM matches)=1
			ORDER BY completed_at DESC LIMIT 1`,
			item.UserID, item.APIKeyID, item.HistoryHash).Scan(&sessionID, &parentID)
		if err == nil {
			mergeSource = "history"
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if sessionID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO conversation_sessions
			(session_uuid,user_id,api_key_id,title,first_model,last_model,merge_source,request_count,total_input_tokens,total_output_tokens,first_request_at,last_request_at)
			VALUES($1,$2,$3,$4,$5,$5,$6,1,$7,$8,$9,$9) RETURNING id`, item.RequestUUID, item.UserID,
			item.APIKeyID, item.Title, item.RequestedModel, mergeSource, item.InputTokens, item.OutputTokens, item.StartedAt).Scan(&sessionID)
		if err != nil {
			return err
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE conversation_sessions SET last_model=$1,merge_source=$2,
			request_count=request_count+1,total_input_tokens=total_input_tokens+$3,total_output_tokens=total_output_tokens+$4,
			last_request_at=$5,updated_at=NOW() WHERE id=$6`, item.RequestedModel, mergeSource, item.InputTokens,
			item.OutputTokens, item.CompletedAt, sessionID)
		if err != nil {
			return err
		}
	}

	requestJSON, err := json.Marshal(item.NormalizedRequest)
	if err != nil {
		return err
	}
	responseJSON, err := json.Marshal(item.NormalizedResponse)
	if err != nil {
		return err
	}
	var requestID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO conversation_requests
		(request_uuid,session_id,parent_request_id,user_id,api_key_id,provider,endpoint,requested_model,stream,status,http_status,
		history_hash,result_hash,input_tokens,output_tokens,duration_ms,request_truncated,response_truncated,started_at,completed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),$14,$15,$16,$17,$18,$19,$20) RETURNING id`,
		item.RequestUUID, sessionID, nullableInt64(parentID), item.UserID, item.APIKeyID, item.Provider, item.Endpoint,
		item.RequestedModel, item.Stream, item.Status, item.HTTPStatus, item.HistoryHash, item.ResultHash, item.InputTokens,
		item.OutputTokens, item.DurationMS, item.RequestTruncated, item.ResponseTruncated, item.StartedAt, item.CompletedAt).Scan(&requestID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO conversation_payloads
		(request_id,raw_request,raw_response,normalized_request,normalized_response,raw_request_bytes,raw_response_bytes)
		VALUES($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7)`, requestID, rawRequest, rawResponse, string(requestJSON), string(responseJSON), item.RawRequestBytes, item.RawResponseBytes)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func (r *ConversationRepository) List(ctx context.Context, filter *service.ConversationFilter) ([]service.ConversationSession, int64, error) {
	where := []string{"s.deleted_at IS NULL"}
	args := []any{}
	add := func(format string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(format, len(args)))
	}
	if filter.UserID > 0 {
		add("s.user_id=$%d", filter.UserID)
	}
	if filter.APIKeyID > 0 {
		add("s.api_key_id=$%d", filter.APIKeyID)
	}
	if filter.Model != "" {
		add("s.last_model ILIKE '%%' || $%d || '%%'", filter.Model)
	}
	if filter.Status != "" {
		add("lr.status=$%d", filter.Status)
	}
	if filter.Query != "" {
		args = append(args, filter.Query)
		p := len(args)
		where = append(where, fmt.Sprintf("(s.title ILIKE '%%' || $%d || '%%' OR u.email ILIKE '%%' || $%d || '%%')", p, p))
	}
	if filter.StartTime != nil {
		add("s.last_request_at >= $%d", *filter.StartTime)
	}
	if filter.EndTime != nil {
		add("s.last_request_at <= $%d", *filter.EndTime)
	}
	base := ` FROM conversation_sessions s JOIN users u ON u.id=s.user_id LEFT JOIN api_keys k ON k.id=s.api_key_id
		LEFT JOIN LATERAL (SELECT status FROM conversation_requests r WHERE r.session_id=s.id ORDER BY r.started_at DESC LIMIT 1) lr ON TRUE
		WHERE ` + strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*)"+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := `SELECT s.id,s.session_uuid::text,s.user_id,u.email,s.api_key_id,COALESCE(k.name,''),s.title,s.first_model,
		s.last_model,s.merge_source,s.request_count,s.total_input_tokens,s.total_output_tokens,s.first_request_at,s.last_request_at,
		COALESCE(lr.status,'')` + base + fmt.Sprintf(" ORDER BY s.last_request_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := []service.ConversationSession{}
	for rows.Next() {
		var item service.ConversationSession
		if err := rows.Scan(&item.ID, &item.SessionUUID, &item.UserID, &item.UserEmail, &item.APIKeyID, &item.APIKeyName, &item.Title,
			&item.FirstModel, &item.LastModel, &item.MergeSource, &item.RequestCount, &item.TotalInputTokens, &item.TotalOutputTokens,
			&item.FirstRequestAt, &item.LastRequestAt, &item.LastStatus); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *ConversationRepository) Get(ctx context.Context, id int64) (*service.ConversationSession, []service.ConversationRequest, error) {
	var session service.ConversationSession
	err := r.db.QueryRowContext(ctx, `SELECT s.id,s.session_uuid::text,s.user_id,u.email,s.api_key_id,COALESCE(k.name,''),s.title,
		s.first_model,s.last_model,s.merge_source,s.request_count,s.total_input_tokens,s.total_output_tokens,s.first_request_at,s.last_request_at,''
		FROM conversation_sessions s JOIN users u ON u.id=s.user_id LEFT JOIN api_keys k ON k.id=s.api_key_id
		WHERE s.id=$1 AND s.deleted_at IS NULL`, id).Scan(&session.ID, &session.SessionUUID, &session.UserID, &session.UserEmail,
		&session.APIKeyID, &session.APIKeyName, &session.Title, &session.FirstModel, &session.LastModel, &session.MergeSource,
		&session.RequestCount, &session.TotalInputTokens, &session.TotalOutputTokens, &session.FirstRequestAt, &session.LastRequestAt, &session.LastStatus)
	if err != nil {
		return nil, nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.id,r.request_uuid::text,r.session_id,r.parent_request_id,r.provider,r.endpoint,
		r.requested_model,r.stream,r.status,r.http_status,r.input_tokens,r.output_tokens,r.duration_ms,r.request_truncated,
		r.response_truncated,r.started_at,r.completed_at,COALESCE(p.normalized_request,'[]'::jsonb),COALESCE(p.normalized_response,'[]'::jsonb)
		FROM conversation_requests r LEFT JOIN conversation_payloads p ON p.request_id=r.id WHERE r.session_id=$1 ORDER BY r.started_at`, id)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []service.ConversationRequest{}
	for rows.Next() {
		var item service.ConversationRequest
		var requestJSON, responseJSON []byte
		if err := rows.Scan(&item.ID, &item.RequestUUID, &item.SessionID, &item.ParentRequestID, &item.Provider, &item.Endpoint,
			&item.RequestedModel, &item.Stream, &item.Status, &item.HTTPStatus, &item.InputTokens, &item.OutputTokens, &item.DurationMS,
			&item.RequestTruncated, &item.ResponseTruncated, &item.StartedAt, &item.CompletedAt, &requestJSON, &responseJSON); err != nil {
			return nil, nil, err
		}
		var requestMessages, responseMessages []service.ConversationMessage
		_ = json.Unmarshal(requestJSON, &requestMessages)
		_ = json.Unmarshal(responseJSON, &responseMessages)
		if requestMessages == nil {
			requestMessages = []service.ConversationMessage{}
		}
		if responseMessages == nil {
			responseMessages = []service.ConversationMessage{}
		}
		if item.ParentRequestID != nil && len(requestMessages) > 0 {
			requestMessages = requestMessages[len(requestMessages)-1:]
		}
		item.Messages = append(requestMessages, responseMessages...)
		items = append(items, item)
	}
	return &session, items, rows.Err()
}

func (r *ConversationRepository) GetRaw(ctx context.Context, requestID int64, response bool) (*service.ConversationRawPayload, error) {
	column, encodingColumn, contentType := "raw_request", "request_encoding", "application/json"
	if response {
		column, encodingColumn, contentType = "raw_response", "response_encoding", "application/octet-stream"
	}
	var data []byte
	var encoding string
	err := r.db.QueryRowContext(ctx, "SELECT "+column+","+encodingColumn+" FROM conversation_payloads WHERE request_id=$1", requestID).Scan(&data, &encoding)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, sql.ErrNoRows
	}
	return &service.ConversationRawPayload{RequestID: requestID, ContentType: contentType, ContentEncoding: encoding, Content: data}, nil
}

func (r *ConversationRepository) DeleteSession(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM conversation_sessions WHERE id=$1", id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *ConversationRepository) ApplyRetention(ctx context.Context, successBefore, failedBefore, textBefore time.Time, limit int) (int64, error) {
	result, err := r.db.ExecContext(ctx, `WITH expired AS (SELECT p.request_id FROM conversation_payloads p JOIN conversation_requests r ON r.id=p.request_id
		WHERE ((r.http_status<400 AND r.completed_at<$1) OR (r.http_status>=400 AND r.completed_at<$2))
		AND (p.raw_request IS NOT NULL OR p.raw_response IS NOT NULL) ORDER BY r.completed_at LIMIT $3 FOR UPDATE OF p SKIP LOCKED)
		UPDATE conversation_payloads p SET raw_request=NULL,raw_response=NULL FROM expired e WHERE p.request_id=e.request_id`, successBefore, failedBefore, limit)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	textResult, err := r.db.ExecContext(ctx, `WITH expired AS (SELECT p.request_id FROM conversation_payloads p JOIN conversation_requests r ON r.id=p.request_id
		WHERE r.completed_at<$1 AND (p.normalized_request IS NOT NULL OR p.normalized_response IS NOT NULL)
		ORDER BY r.completed_at LIMIT $2 FOR UPDATE OF p SKIP LOCKED)
		UPDATE conversation_payloads p SET normalized_request=NULL,normalized_response=NULL FROM expired e WHERE p.request_id=e.request_id`, textBefore, limit)
	if err != nil {
		return affected, err
	}
	textAffected, _ := textResult.RowsAffected()
	return affected + textAffected, nil
}

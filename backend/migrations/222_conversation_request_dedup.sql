ALTER TABLE conversation_requests
    ADD COLUMN IF NOT EXISTS request_fingerprint TEXT;

UPDATE conversation_requests r
SET request_fingerprint = md5(p.normalized_request::text)
FROM conversation_payloads p
WHERE p.request_id = r.id
  AND r.request_fingerprint IS NULL;

CREATE INDEX IF NOT EXISTS idx_conversation_requests_fingerprint_time
    ON conversation_requests(user_id, api_key_id, endpoint, requested_model, request_fingerprint, started_at DESC)
    WHERE request_fingerprint IS NOT NULL;

-- Fold historical retries into their earliest session while retaining every request and payload.
WITH ordered AS (
    SELECT r.id,
           r.session_id,
           r.user_id,
           r.api_key_id,
           r.endpoint,
           r.requested_model,
           r.request_fingerprint,
           r.started_at,
           CASE WHEN lag(r.started_at) OVER fingerprint_window IS NULL
                     OR r.started_at - lag(r.started_at) OVER fingerprint_window > INTERVAL '10 minutes'
                THEN 1 ELSE 0 END AS starts_group
    FROM conversation_requests r
    JOIN conversation_sessions s ON s.id = r.session_id
    WHERE r.request_fingerprint IS NOT NULL
      AND s.request_count = 1
    WINDOW fingerprint_window AS (
        PARTITION BY r.user_id, r.api_key_id, r.endpoint, r.requested_model, r.request_fingerprint
        ORDER BY r.started_at, r.id
    )
), grouped AS (
    SELECT *, sum(starts_group) OVER (
        PARTITION BY user_id, api_key_id, endpoint, requested_model, request_fingerprint
        ORDER BY started_at, id
    ) AS retry_group
    FROM ordered
), targets AS (
    SELECT id,
           first_value(session_id) OVER (
               PARTITION BY user_id, api_key_id, endpoint, requested_model, request_fingerprint, retry_group
               ORDER BY started_at, id
           ) AS target_session_id
    FROM grouped
)
UPDATE conversation_requests r
SET session_id = t.target_session_id
FROM targets t
WHERE r.id = t.id
  AND r.session_id <> t.target_session_id;

DELETE FROM conversation_sessions s
WHERE NOT EXISTS (SELECT 1 FROM conversation_requests r WHERE r.session_id = s.id);

WITH stats AS (
    SELECT r.session_id,
           count(*) AS request_count,
           sum(r.input_tokens) AS total_input_tokens,
           sum(r.output_tokens) AS total_output_tokens,
           min(r.started_at) AS first_request_at,
           max(r.completed_at) AS last_request_at,
           (array_agg(r.requested_model ORDER BY r.started_at, r.id))[1] AS first_model,
           (array_agg(r.requested_model ORDER BY r.started_at DESC, r.id DESC))[1] AS last_model
    FROM conversation_requests r
    GROUP BY r.session_id
)
UPDATE conversation_sessions s
SET request_count = stats.request_count,
    total_input_tokens = stats.total_input_tokens,
    total_output_tokens = stats.total_output_tokens,
    first_request_at = stats.first_request_at,
    last_request_at = stats.last_request_at,
    first_model = stats.first_model,
    last_model = stats.last_model,
    merge_source = CASE WHEN stats.request_count > 1 THEN 'duplicate' ELSE s.merge_source END,
    updated_at = NOW()
FROM stats
WHERE stats.session_id = s.id;

CREATE TABLE IF NOT EXISTS conversation_sessions (
    id BIGSERIAL PRIMARY KEY,
    session_uuid UUID NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    first_model VARCHAR(200) NOT NULL DEFAULT '',
    last_model VARCHAR(200) NOT NULL DEFAULT '',
    merge_source VARCHAR(16) NOT NULL DEFAULT 'isolated',
    request_count INTEGER NOT NULL DEFAULT 0,
    total_input_tokens BIGINT NOT NULL DEFAULT 0,
    total_output_tokens BIGINT NOT NULL DEFAULT 0,
    first_request_at TIMESTAMPTZ NOT NULL,
    last_request_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_conversation_sessions_user_time ON conversation_sessions(user_id,last_request_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS conversation_requests (
    id BIGSERIAL PRIMARY KEY,
    request_uuid UUID NOT NULL UNIQUE,
    session_id BIGINT NOT NULL REFERENCES conversation_sessions(id) ON DELETE CASCADE,
    parent_request_id BIGINT REFERENCES conversation_requests(id) ON DELETE SET NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    provider VARCHAR(32) NOT NULL DEFAULT '', endpoint VARCHAR(160) NOT NULL,
    requested_model VARCHAR(200) NOT NULL DEFAULT '', stream BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(32) NOT NULL, http_status INTEGER NOT NULL,
    history_hash CHAR(64), result_hash CHAR(64), input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0, duration_ms BIGINT NOT NULL DEFAULT 0,
    request_truncated BOOLEAN NOT NULL DEFAULT FALSE, response_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    started_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_conversation_requests_result_hash ON conversation_requests(user_id,api_key_id,result_hash,completed_at DESC) WHERE result_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_conversation_requests_session_time ON conversation_requests(session_id,started_at);

CREATE TABLE IF NOT EXISTS conversation_payloads (
    request_id BIGINT PRIMARY KEY REFERENCES conversation_requests(id) ON DELETE CASCADE,
    raw_request BYTEA, raw_response BYTEA, normalized_request JSONB, normalized_response JSONB,
    request_encoding VARCHAR(32) NOT NULL DEFAULT 'zstd', response_encoding VARCHAR(32) NOT NULL DEFAULT 'zstd',
    raw_request_bytes BIGINT NOT NULL DEFAULT 0, raw_response_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

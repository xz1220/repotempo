-- Signing keys are sensitive HMAC credentials, even though the issued secret
-- itself is not retained. Protect this database and its backups accordingly.
CREATE TABLE agent_keys (
    id TEXT PRIMARY KEY NOT NULL,
    github_user_id INTEGER NOT NULL REFERENCES users(github_user_id),
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
    scopes TEXT NOT NULL,
    signing_key BLOB NOT NULL CHECK(length(signing_key)=32),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL CHECK(expires_at > created_at),
    last_used_at TEXT,
    revoked_at TEXT
);
CREATE INDEX agent_keys_owner ON agent_keys(github_user_id,created_at);
CREATE TABLE agent_request_nonces (
    key_id TEXT NOT NULL REFERENCES agent_keys(id) ON DELETE CASCADE,
    nonce TEXT NOT NULL,
    accepted_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY(key_id,nonce)
);
CREATE INDEX agent_request_nonces_expiry ON agent_request_nonces(expires_at);
CREATE INDEX agent_request_nonces_rate ON agent_request_nonces(key_id,accepted_at);

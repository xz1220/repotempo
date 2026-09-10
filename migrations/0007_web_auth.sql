-- Authentication is separate from project data. Browser session tokens and
-- GitHub OAuth access tokens/client secrets are never persisted here.
CREATE TABLE oauth_login_states (
    state_hash TEXT NOT NULL PRIMARY KEY CHECK(length(state_hash)=64 AND state_hash NOT GLOB '*[^0-9a-f]*'),
    binding_hash TEXT NOT NULL CHECK(length(binding_hash)=64 AND binding_hash NOT GLOB '*[^0-9a-f]*'),
    verifier TEXT NOT NULL CHECK(length(verifier) BETWEEN 43 AND 128 AND verifier NOT GLOB '*[^A-Za-z0-9._~-]*'),
    return_path TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL CHECK(expires_at > created_at)
);
CREATE INDEX oauth_login_states_expiry ON oauth_login_states(expires_at);

CREATE TABLE web_auth_sessions (
    token_hash TEXT NOT NULL PRIMARY KEY CHECK(length(token_hash)=64 AND token_hash NOT GLOB '*[^0-9a-f]*'),
    github_user_id INTEGER NOT NULL CHECK(github_user_id > 0),
    login TEXT NOT NULL,
    csrf_token TEXT NOT NULL CHECK(length(csrf_token)=64 AND csrf_token NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL CHECK(expires_at > created_at)
);
CREATE INDEX web_auth_sessions_expiry ON web_auth_sessions(expires_at);
CREATE INDEX web_auth_sessions_user_created ON web_auth_sessions(github_user_id,created_at);

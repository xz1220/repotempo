CREATE TABLE users (
    github_user_id INTEGER PRIMARY KEY CHECK(github_user_id > 0),
    login TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO users (github_user_id, login, created_at, updated_at)
SELECT github_user_id, login, MIN(created_at), MAX(created_at)
FROM web_auth_sessions GROUP BY github_user_id;

CREATE TABLE user_repositories (
    user_id INTEGER NOT NULL REFERENCES users(github_user_id),
    repository_id INTEGER NOT NULL REFERENCES repositories(github_repo_id),
    is_focus INTEGER NOT NULL DEFAULT 0 CHECK(is_focus IN (0,1)),
    note TEXT NOT NULL DEFAULT '' CHECK(length(note) <= 2000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, repository_id)
);
CREATE INDEX user_repositories_focus ON user_repositories(user_id, is_focus, repository_id);

CREATE TABLE account_migrations (
    name TEXT PRIMARY KEY,
    owner_id INTEGER NOT NULL REFERENCES users(github_user_id),
    created_at TEXT NOT NULL
);

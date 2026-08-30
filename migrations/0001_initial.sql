CREATE TABLE IF NOT EXISTS repositories (
    github_repo_id         INTEGER PRIMARY KEY CHECK (github_repo_id > 0),
    github_node_id         TEXT NOT NULL DEFAULT '',
    full_name              TEXT NOT NULL CHECK (length(trim(full_name)) > 0),
    html_url               TEXT NOT NULL DEFAULT '',
    description            TEXT NOT NULL DEFAULT '',
    primary_language       TEXT NOT NULL DEFAULT '',
    github_created_at      TEXT,
    first_seen_at          TEXT NOT NULL,
    first_seen_source      TEXT NOT NULL CHECK (first_seen_source IN ('ossinsight', 'github_search', 'legacy', 'manual')),
    first_seen_profile     TEXT NOT NULL DEFAULT '',
    discovery_sources_json TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(discovery_sources_json) AND json_type(discovery_sources_json) = 'array'),
    last_discovered_at     TEXT NOT NULL,
    monitoring_status      TEXT NOT NULL DEFAULT 'active'
        CHECK (monitoring_status IN ('active', 'paused', 'stopped')),
    github_status          TEXT NOT NULL DEFAULT 'active'
        CHECK (github_status IN ('active', 'archived', 'deleted', 'private', 'unreachable')),
    is_focus               INTEGER NOT NULL DEFAULT 0 CHECK (is_focus IN (0, 1)),
    manual_note            TEXT NOT NULL DEFAULT '',
    github_etag            TEXT NOT NULL DEFAULT '',
    last_checked_at        TEXT,
    previous_names_json    TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(previous_names_json) AND json_type(previous_names_json) = 'array'),
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS daily_snapshots (
    repository_id   INTEGER NOT NULL REFERENCES repositories(github_repo_id) ON DELETE CASCADE,
    snapshot_date   TEXT NOT NULL CHECK (
        length(snapshot_date) = 10 AND
        snapshot_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    ),
    captured_at     TEXT NOT NULL,
    star_count      INTEGER CHECK (star_count IS NULL OR star_count >= 0),
    fetch_status    TEXT NOT NULL CHECK (fetch_status IN ('success', 'failed')),
    http_status     INTEGER CHECK (http_status IS NULL OR http_status BETWEEN 100 AND 599),
    error_code      TEXT NOT NULL DEFAULT '',
    oss_today_rank  INTEGER CHECK (oss_today_rank IS NULL OR oss_today_rank > 0),
    oss_window_stars INTEGER CHECK (oss_window_stars IS NULL OR oss_window_stars >= 0),
    oss_total_score REAL,
    created_at      TEXT NOT NULL,
    PRIMARY KEY (repository_id, snapshot_date),
    CHECK (
        (fetch_status = 'success' AND star_count IS NOT NULL) OR
        (fetch_status = 'failed' AND star_count IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS topics (
    id          INTEGER PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(trim(slug)) > 0),
    name        TEXT NOT NULL CHECK (length(trim(name)) > 0),
    parent_id   INTEGER REFERENCES topics(id) ON DELETE RESTRICT,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE TABLE IF NOT EXISTS repository_topics (
    repository_id INTEGER NOT NULL REFERENCES repositories(github_repo_id) ON DELETE CASCADE,
    topic_id      INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    source        TEXT NOT NULL CHECK (source IN ('manual', 'github', 'imported', 'auto')),
    confirmed     INTEGER NOT NULL DEFAULT 0 CHECK (confirmed IN (0, 1)),
    confidence    REAL CHECK (confidence IS NULL OR confidence BETWEEN 0.0 AND 1.0),
    assigned_at   TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (repository_id, topic_id)
);

CREATE TABLE IF NOT EXISTS job_runs (
    run_id        TEXT PRIMARY KEY CHECK (length(trim(run_id)) > 0),
    job_type      TEXT NOT NULL CHECK (length(trim(job_type)) > 0),
    started_at    TEXT NOT NULL,
    finished_at   TEXT,
    status        TEXT NOT NULL CHECK (status IN ('running', 'success', 'partial', 'failed')),
    target_count  INTEGER NOT NULL DEFAULT 0 CHECK (target_count >= 0),
    success_count INTEGER NOT NULL DEFAULT 0 CHECK (success_count >= 0),
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    skipped_count INTEGER NOT NULL DEFAULT 0 CHECK (skipped_count >= 0),
    details_json  TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(details_json) AND json_type(details_json) = 'object'),
    error_summary TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_repositories_full_name_nocase
    ON repositories(full_name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_repositories_monitoring
    ON repositories(monitoring_status, github_status);
CREATE INDEX IF NOT EXISTS idx_daily_snapshots_date_status
    ON daily_snapshots(snapshot_date, fetch_status);
CREATE INDEX IF NOT EXISTS idx_repository_topics_topic
    ON repository_topics(topic_id, repository_id);
CREATE INDEX IF NOT EXISTS idx_job_runs_started_at
    ON job_runs(started_at DESC);

CREATE TRIGGER IF NOT EXISTS topics_two_levels_insert
BEFORE INSERT ON topics
WHEN NEW.parent_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN (SELECT parent_id FROM topics WHERE id = NEW.parent_id) IS NOT NULL
        THEN RAISE(ABORT, 'topic hierarchy supports at most two levels')
    END;
END;

CREATE TRIGGER IF NOT EXISTS topics_two_levels_update
BEFORE UPDATE OF parent_id ON topics
WHEN NEW.parent_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN NEW.parent_id = NEW.id
          OR (SELECT parent_id FROM topics WHERE id = NEW.parent_id) IS NOT NULL
          OR EXISTS (SELECT 1 FROM topics WHERE parent_id = NEW.id)
        THEN RAISE(ABORT, 'topic hierarchy supports at most two levels')
    END;
END;

-- This parent-table rebuild MUST be run through the migration runner. The
-- runner disables foreign keys outside the transaction on a pinned connection,
-- restores repository indexes/triggers, and verifies the preexisting foreign
-- key violation set is unchanged before committing. Child tables stay in place.
CREATE TABLE repositories_v4 (
    github_repo_id         INTEGER PRIMARY KEY CHECK (github_repo_id > 0),
    github_node_id         TEXT NOT NULL DEFAULT '',
    full_name              TEXT NOT NULL CHECK (length(trim(full_name)) > 0),
    html_url               TEXT NOT NULL DEFAULT '',
    description            TEXT NOT NULL DEFAULT '',
    primary_language       TEXT NOT NULL DEFAULT '',
    github_created_at      TEXT,
    first_seen_at          TEXT NOT NULL,
    first_seen_source      TEXT NOT NULL CHECK (first_seen_source IN ('ossinsight', 'github_search', 'github_trending', 'legacy', 'manual')),
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

INSERT INTO repositories_v4 (
    github_repo_id, github_node_id, full_name, html_url, description,
    primary_language, github_created_at, first_seen_at, first_seen_source,
    first_seen_profile, discovery_sources_json, last_discovered_at,
    monitoring_status, github_status, is_focus, manual_note, github_etag,
    last_checked_at, previous_names_json, created_at, updated_at
)
SELECT
    github_repo_id, github_node_id, full_name, html_url, description,
    primary_language, github_created_at, first_seen_at, first_seen_source,
    first_seen_profile, discovery_sources_json, last_discovered_at,
    monitoring_status, github_status, is_focus, manual_note, github_etag,
    last_checked_at, previous_names_json, created_at, updated_at
FROM repositories;

DROP TABLE repositories;
ALTER TABLE repositories_v4 RENAME TO repositories;

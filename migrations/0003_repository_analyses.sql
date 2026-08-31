CREATE TABLE IF NOT EXISTS repository_analyses (
    repository_id   INTEGER PRIMARY KEY
        REFERENCES repositories(github_repo_id) ON DELETE CASCADE,
    summary_zh      TEXT NOT NULL DEFAULT '',
    key_points_json TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(key_points_json) AND json_type(key_points_json) = 'array'),
    use_cases_json  TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(use_cases_json) AND json_type(use_cases_json) = 'array'),
    technical_notes TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL CHECK (length(trim(source)) > 0),
    model           TEXT NOT NULL DEFAULT '',
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    analyzed_at     TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS repository_analyses_string_arrays_insert
BEFORE INSERT ON repository_analyses
WHEN EXISTS (
    SELECT 1 FROM json_each(NEW.key_points_json) WHERE type <> 'text'
) OR EXISTS (
    SELECT 1 FROM json_each(NEW.use_cases_json) WHERE type <> 'text'
)
BEGIN
    SELECT RAISE(ABORT, 'repository analysis lists must contain only strings');
END;

CREATE TRIGGER IF NOT EXISTS repository_analyses_string_arrays_update
BEFORE UPDATE OF key_points_json, use_cases_json ON repository_analyses
WHEN EXISTS (
    SELECT 1 FROM json_each(NEW.key_points_json) WHERE type <> 'text'
) OR EXISTS (
    SELECT 1 FROM json_each(NEW.use_cases_json) WHERE type <> 'text'
)
BEGIN
    SELECT RAISE(ABORT, 'repository analysis lists must contain only strings');
END;

INSERT INTO repository_analyses (
    repository_id,
    summary_zh,
    key_points_json,
    use_cases_json,
    technical_notes,
    source,
    model,
    revision,
    analyzed_at,
    created_at,
    updated_at
)
SELECT
    github_repo_id,
    manual_note,
    '[]',
    '[]',
    '',
    'imported',
    '',
    1,
    updated_at,
    created_at,
    updated_at
FROM repositories
WHERE length(trim(
    manual_note,
    char(9) || char(10) || char(11) || char(12) || char(13) || char(32)
)) > 0
ON CONFLICT(repository_id) DO NOTHING;

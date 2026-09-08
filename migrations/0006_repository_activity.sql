-- Commit activity is an independent, replaceable cache, not a Star snapshot
-- or generated analysis. Existing rows remain unknown until actually fetched.
ALTER TABLE repositories ADD COLUMN activity_json TEXT
    CHECK (activity_json IS NULL OR
        (json_valid(activity_json) AND json_type(activity_json) = 'object'));

ALTER TABLE repositories ADD COLUMN github_topics_json TEXT
    CHECK (github_topics_json IS NULL OR
        (json_valid(github_topics_json) AND json_type(github_topics_json) = 'array'));
ALTER TABLE repositories ADD COLUMN research_tags_json TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(research_tags_json) AND json_type(research_tags_json) = 'array');

CREATE TRIGGER repositories_tag_arrays_insert
BEFORE INSERT ON repositories
WHEN EXISTS (SELECT 1 FROM json_each(NEW.github_topics_json) WHERE type <> 'text')
  OR EXISTS (SELECT 1 FROM json_each(NEW.research_tags_json) WHERE type <> 'text')
BEGIN
    SELECT RAISE(ABORT, 'repository tags must contain only strings');
END;

CREATE TRIGGER repositories_tag_arrays_update
BEFORE UPDATE OF github_topics_json, research_tags_json ON repositories
WHEN EXISTS (SELECT 1 FROM json_each(NEW.github_topics_json) WHERE type <> 'text')
  OR EXISTS (SELECT 1 FROM json_each(NEW.research_tags_json) WHERE type <> 'text')
BEGIN
    SELECT RAISE(ABORT, 'repository tags must contain only strings');
END;

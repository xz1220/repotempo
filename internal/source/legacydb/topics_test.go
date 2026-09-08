package legacydb

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestLegacyTopicsOnlyUseExplicitRawColumnsAndKeepUnknownState(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE repos (repo_id INTEGER,repo_name TEXT,primary_language TEXT,description TEXT,html_url TEXT,github_topics TEXT,github_status TEXT,archived INTEGER,metadata_etag TEXT,first_seen_at TEXT,last_seen_at TEXT,is_active INTEGER,manual_favorite INTEGER,topics TEXT,research_tags_json TEXT);
INSERT INTO repos (repo_id,repo_name,github_topics,topics,research_tags_json) VALUES (1,'o/unknown',NULL,'["AI Agent"]','["中文研究"]'),(2,'o/empty','[]','["AI Agent"]',NULL);`); err != nil {
		t.Fatal(err)
	}
	candidates, _, err := readRepositories(context.Background(), db, map[int64]catalogEntry{}, time.UTC)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}
	for _, candidate := range candidates {
		if candidate.Repository.ID == 1 && (candidate.Repository.Topics != nil || len(candidate.Repository.ResearchTags) != 1 || candidate.Repository.ResearchTags[0] != "中文研究") {
			t.Fatal("legacy taxonomy was treated as raw GitHub tags")
		}
		if candidate.Repository.ID == 2 && (candidate.Repository.Topics == nil || len(candidate.Repository.Topics) != 0) {
			t.Fatal("legacy known empty raw tags became unknown")
		}
	}
}

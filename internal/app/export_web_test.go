package app

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

func TestWebCSVExportsBeyondInteractivePageAndReadmeLimit(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for id := int64(1); id <= 101; id++ {
		if _, _, err := db.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: id, FullName: fmt.Sprintf("export/project-%d", id), Source: domain.DiscoverySourceGitHubSearch, DiscoveredAt: now.Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	h, err := web.New(WebAdapter{Store: db}, web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/repositories/export?view=all&new=0", nil))
	if r.Code != 200 {
		t.Fatalf("large CSV status=%d", r.Code)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.Body.String(), "\ufeff"))).ReadAll()
	if err != nil || len(rows) != 102 {
		t.Fatalf("CSV rows=%d error=%v", len(rows), err)
	}
}

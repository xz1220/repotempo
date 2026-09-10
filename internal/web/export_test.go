package web

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type exportQueryer struct {
	*fakeQueryer
	offsets []int
}

func (q *exportQueryer) ListRepositoryTrends(_ context.Context, filter RepositoryQuery) (RepositoryPage, error) {
	q.offsets = append(q.offsets, filter.Offset)
	page := RepositoryPage{Total: 101, Coverage: ComparisonCoverage{AsOfDate: mustDate("2026-09-10")}}
	for i := filter.Offset; i < 101 && i < filter.Offset+filter.Limit; i++ {
		page.Items = append(page.Items, RepositoryMetric{ID: int64(i + 1), FullName: fmt.Sprintf("team/repo-%d", i), Description: "=untrusted formula", ManualNote: "PRIVATE-DO-NOT-EXPORT"})
	}
	page.HasMore = filter.Offset+len(page.Items) < 101
	return page, nil
}
func TestCSVExportUsesAllServerPagesAndOmitsPrivateNotes(t *testing.T) {
	q := &exportQueryer{fakeQueryer: populatedFake()}
	h := newTestHandler(t, q)
	r := request(t, h, "/repositories/export?view=all&new=0&lang=en")
	if r.Code != http.StatusOK {
		t.Fatalf("export status %d", r.Code)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.Body.String(), "\ufeff"))).ReadAll()
	if err != nil || len(rows) != 102 || len(q.offsets) != 2 || q.offsets[1] != 100 {
		t.Fatalf("incomplete export rows=%d offsets=%v err=%v", len(rows), q.offsets, err)
	}
	if rows[1][2] != "'=untrusted formula" || strings.Contains(r.Body.String(), "PRIVATE-DO-NOT-EXPORT") {
		t.Fatal("export leaks private note or active spreadsheet formula")
	}
}

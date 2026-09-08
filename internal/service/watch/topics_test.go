package watch

import (
	"context"
	"github.com/xz1220/repotempo/internal/source"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"reflect"
	"testing"
)

func TestCLIAndTrackedWatchPersistTopicsIncludingKnownEmpty(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stars := int64(100)
	resolver := &trackedResolver{repository: source.Repository{ID: 9, FullName: "owner/tags", Topics: []string{" RAG "}, AbsoluteStars: &stars}}
	service := Service{Store: db, Resolver: resolver}
	if _, err := service.Add(ctx, "owner/tags", "", true, false); err != nil {
		t.Fatal(err)
	}
	repository, err := db.GetRepository(ctx, 9)
	if err != nil || !reflect.DeepEqual(repository.GitHubTopics, []string{"rag"}) {
		t.Fatalf("CLI watch tags=%v err=%v", repository.GitHubTopics, err)
	}
	resolver.repository.Topics = []string{}
	if _, err := service.AddTracked(ctx, "owner/tags", "", ""); err != nil {
		t.Fatal(err)
	}
	repository, err = db.GetRepository(ctx, 9)
	if err != nil || repository.GitHubTopics == nil || len(repository.GitHubTopics) != 0 {
		t.Fatal("tracked/Web watch did not persist known empty topics")
	}
}

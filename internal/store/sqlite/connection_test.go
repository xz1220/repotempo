package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

func discardPhysicalConnection(t *testing.T, db *sql.DB) any {
	t.Helper()
	connection, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var original any
	err = connection.Raw(func(value any) error {
		original = value
		return driver.ErrBadConn
	})
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("force discard returned %v", err)
	}
	_ = connection.Close()
	return original
}

func assertConnectionPolicy(t *testing.T, db *sql.DB, timeoutMS int, previous any) {
	t.Helper()
	connection, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if previous != nil {
		if err := connection.Raw(func(current any) error {
			if current == previous {
				t.Error("test did not replace the physical connection")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, setting := range []struct {
		name  string
		value int
	}{{"foreign_keys", 1}, {"busy_timeout", timeoutMS}, {"synchronous", 1}} {
		var actual int
		if err := connection.QueryRowContext(context.Background(), "PRAGMA "+setting.name).Scan(&actual); err != nil || actual != setting.value {
			t.Errorf("replacement connection %s = %d, error %v; want %d", setting.name, actual, err, setting.value)
		}
	}
}

func TestStoreConnectionPolicySurvivesPhysicalConnectionReplacement(t *testing.T) {
	store, _ := newTestStore(t)
	addRepository(t, store, 1, "owner/preserved")
	assertConnectionPolicy(t, store.db, 5000, nil)
	previous := discardPhysicalConnection(t, store.db)
	assertConnectionPolicy(t, store.db, 5000, previous)
	if _, err := store.GetRepository(context.Background(), 1); err != nil {
		t.Fatalf("physical replacement lost the persisted registry: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO daily_snapshots (repository_id, snapshot_date, captured_at, star_count, fetch_status, created_at)
VALUES (999999, '2026-09-08', '2026-09-08T00:00:00Z', 1, 'success', '2026-09-08T00:00:00Z')`); err == nil {
		t.Fatal("replacement connection accepted an orphan snapshot")
	}
}

func TestStoreConnectionFactoryPreservesMemoryAndEscapedFilePaths(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{":memory:", filepath.Join(t.TempDir(), "观察 ? # % space.db")} {
		store, err := OpenWithConfig(ctx, Config{Path: path, BusyTimeout: 1730 * time.Millisecond})
		if err != nil {
			t.Fatalf("open %q: %v", path, err)
		}
		if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 1, FullName: "owner/path", Source: domain.DiscoverySourceManual}); err != nil {
			t.Fatal(err)
		}
		assertConnectionPolicy(t, store.db, 1730, nil)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		if path != ":memory:" {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("the exact requested file path was not created: %v", err)
			}
		}
	}
}

func TestStoreConnectionFactoryPreservesURIQueriesAndReadOnlyMode(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "URI % 观察.db")
	uri := &url.URL{Scheme: "file", Path: path}
	uri.RawQuery = "mode=rwc&_foreign_keys=off&_busy_timeout=1&_synchronous=FULL"
	store, err := OpenWithConfig(ctx, Config{Path: uri.String(), BusyTimeout: 1200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	assertConnectionPolicy(t, store.db, 1200, nil)
	previous := discardPhysicalConnection(t, store.db)
	assertConnectionPolicy(t, store.db, 1200, previous)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	uri.RawQuery = "mode=ro"
	readOnly, err := Open(ctx, uri.String())
	if err != nil {
		t.Fatalf("open existing read-only URI: %v", err)
	}
	defer readOnly.Close()
	assertConnectionPolicy(t, readOnly.db, 5000, nil)
	if _, err := readOnly.db.Exec("CREATE TABLE forbidden_write (id INTEGER)"); err == nil {
		t.Fatal("URI mode=ro was lost")
	}
	missingDirectory := filepath.Join(t.TempDir(), "not-created")
	uri.Path = filepath.Join(missingDirectory, "missing.db")
	if unexpected, err := Open(ctx, uri.String()); err == nil {
		unexpected.Close()
		t.Fatal("missing read-only URI unexpectedly opened")
	}
	if _, err := os.Stat(missingDirectory); !os.IsNotExist(err) {
		t.Fatalf("read-only URI created a directory: %v", err)
	}
}

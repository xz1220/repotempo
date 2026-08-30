package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xz1220/github-radar/internal/config"
	"github.com/xz1220/github-radar/internal/exporter"
	"github.com/xz1220/github-radar/internal/service/discovery"
	"github.com/xz1220/github-radar/internal/service/jobs"
	"github.com/xz1220/github-radar/internal/service/snapshot"
	"github.com/xz1220/github-radar/internal/service/topic"
	"github.com/xz1220/github-radar/internal/service/watch"
	"github.com/xz1220/github-radar/internal/source/github"
	"github.com/xz1220/github-radar/internal/source/ossinsight"
	"github.com/xz1220/github-radar/internal/store/sqlite"
)

type RuntimeOptions struct {
	Now          func() time.Time
	HTTPClient   *http.Client
	Logger       *slog.Logger
	CheckRuntime func(Settings) DoctorReport
	ReadOnly     bool
}

type Runtime struct {
	settings     Settings
	store        *sqlite.Store
	planningDB   *sql.DB
	now          func() time.Time
	httpClient   *http.Client
	logger       *slog.Logger
	checkRuntime func(Settings) DoctorReport

	dependencyMu sync.Mutex
	loaded       bool
	discovery    *config.Discovery
	github       *github.Client
	oss          *ossinsight.Client
}

var _ CommandApplication = (*Runtime)(nil)

func OpenRuntime(ctx context.Context, settings Settings) (*Runtime, error) {
	return OpenRuntimeWithOptions(ctx, settings, RuntimeOptions{})
}

func OpenPlanningRuntime(ctx context.Context, settings Settings) (*Runtime, error) {
	return OpenRuntimeWithOptions(ctx, settings, RuntimeOptions{ReadOnly: true})
}

func OpenRuntimeWithOptions(ctx context.Context, settings Settings, options RuntimeOptions) (*Runtime, error) {
	if options.ReadOnly {
		return openPlanningRuntime(ctx, settings, options)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.CheckRuntime == nil {
		options.CheckRuntime = CheckRuntime
	}
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: settings.DatabasePath, Now: options.Now})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		settings:     settings,
		store:        store,
		now:          options.Now,
		httpClient:   options.HTTPClient,
		logger:       options.Logger,
		checkRuntime: options.CheckRuntime,
	}, nil
}

func openPlanningRuntime(ctx context.Context, settings Settings, options RuntimeOptions) (*Runtime, error) {
	targetPath := settings.DatabasePath
	memorySettings := settings
	memorySettings.DatabasePath = ":memory:"
	options.ReadOnly = false
	runtime, err := OpenRuntimeWithOptions(ctx, memorySettings, options)
	if err != nil {
		return nil, err
	}
	// Runtime configuration, diagnostics, and source paths still refer to the
	// actual target; only business storage is redirected to memory.
	runtime.settings = settings
	if targetPath == "" || targetPath == ":memory:" {
		return runtime, nil
	}
	absolute, err := filepath.Abs(targetPath)
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("resolve planning database path: %w", err)
	}
	info, err := os.Stat(absolute)
	if os.IsNotExist(err) {
		return runtime, nil
	}
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("inspect planning database: %w", err)
	}
	if info.IsDir() {
		_ = runtime.Close()
		return nil, fmt.Errorf("planning database path is a directory")
	}
	query := url.Values{"mode": []string{"ro"}}
	if _, walErr := os.Stat(absolute + "-wal"); os.IsNotExist(walErr) {
		// Immutable mode guarantees that a closed, checkpointed database does
		// not gain journal or shared-memory sidecars during a dry-run.
		query.Set("immutable", "1")
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: query.Encode()}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("open planning database read-only: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	closeOnError := func(cause error) (*Runtime, error) {
		_ = database.Close()
		_ = runtime.Close()
		return nil, cause
	}
	if err := database.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping planning database: %w", err))
	}
	if _, err := database.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		return closeOnError(fmt.Errorf("enable planning query-only mode: %w", err))
	}
	runtime.planningDB = database
	return runtime, nil
}

func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	var planningErr, storeErr error
	if runtime.planningDB != nil {
		planningErr = runtime.planningDB.Close()
	}
	if runtime.store != nil {
		storeErr = runtime.store.Close()
	}
	return errors.Join(planningErr, storeErr)
}

func (runtime *Runtime) dependencies() (config.Discovery, *github.Client, *ossinsight.Client, error) {
	runtime.dependencyMu.Lock()
	defer runtime.dependencyMu.Unlock()
	if runtime.loaded {
		return *runtime.discovery, runtime.github, runtime.oss, nil
	}

	discoveryConfig, err := config.LoadDiscovery(runtime.settings.DiscoveryConfig)
	if err != nil {
		return config.Discovery{}, nil, nil, err
	}
	githubClient, err := github.NewClient(github.ClientOptions{
		BaseURL:          discoveryConfig.GitHub.APIBaseURL,
		Token:            runtime.settings.GitHubToken,
		APIVersion:       discoveryConfig.GitHub.APIVersion,
		UserAgent:        discoveryConfig.GitHub.UserAgent,
		HTTPClient:       runtime.httpClient,
		Timeout:          discoveryConfig.GitHub.Timeout.Value(),
		CoreInterval:     discoveryConfig.GitHub.CoreInterval.Value(),
		SearchInterval:   discoveryConfig.GitHub.SearchInterval.Value(),
		SecondaryBackoff: discoveryConfig.GitHub.SecondaryBackoff.Value(),
		ServerBackoff:    discoveryConfig.GitHub.ServerBackoff.Value(),
		MaxRetries:       discoveryConfig.GitHub.MaxRetries,
		Now:              runtime.now,
	})
	if err != nil {
		return config.Discovery{}, nil, nil, fmt.Errorf("configure GitHub client: %w", err)
	}
	var ossClient *ossinsight.Client
	if discoveryConfig.OSSInsight.Enabled == nil || *discoveryConfig.OSSInsight.Enabled {
		ossClient, err = ossinsight.NewClient(ossinsight.ClientOptions{
			BaseURL:    discoveryConfig.OSSInsight.BaseURL,
			HTTPClient: runtime.httpClient,
			Timeout:    discoveryConfig.OSSInsight.Timeout.Value(),
			Now:        runtime.now,
		})
		if err != nil {
			return config.Discovery{}, nil, nil, fmt.Errorf("configure OSS Insight client: %w", err)
		}
	}
	runtime.loaded = true
	runtime.discovery = &discoveryConfig
	runtime.github = githubClient
	runtime.oss = ossClient
	return discoveryConfig, githubClient, ossClient, nil
}

func (runtime *Runtime) tracker() jobs.Tracker {
	return jobs.Tracker{Store: runtime.store, Now: runtime.now}
}

func (runtime *Runtime) registry() discovery.Registry {
	return discovery.Registry{Store: runtime.store, Now: runtime.now}
}

func (runtime *Runtime) snapshotService(client *github.Client) snapshot.Service {
	return snapshot.Service{Store: runtime.store, GitHub: client, Now: runtime.now}
}

func (runtime *Runtime) topicService() topic.Service {
	return topic.Service{Store: runtime.store, Now: runtime.now}
}

func (runtime *Runtime) watchService(client *github.Client) watch.Service {
	return watch.Service{Store: runtime.store, Resolver: client, Now: runtime.now}
}

func (runtime *Runtime) dataExporter() exporter.Exporter {
	return exporter.Exporter{Source: runtime.store, Backup: runtime.store, Now: runtime.now}
}

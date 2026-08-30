package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/service/snapshot"
)

type RuntimeFactory func(context.Context, Settings) (CommandApplication, error)

type CLI struct {
	Stdout              io.Writer
	Stderr              io.Writer
	LoadSettings        func() (Settings, error)
	OpenRuntime         RuntimeFactory
	OpenPlanningRuntime RuntimeFactory
	CheckRuntime        func(Settings) DoctorReport
}

type globalOptions struct {
	JSON            bool
	DatabasePath    string
	DiscoveryConfig string
}

type outputEnvelope struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

type commandAction func(CommandApplication) (value any, human string, code int, err error)

func NewCLI() *CLI {
	return &CLI{
		Stdout:       io.Discard,
		Stderr:       io.Discard,
		LoadSettings: LoadSettings,
		OpenRuntime: func(ctx context.Context, settings Settings) (CommandApplication, error) {
			return OpenRuntime(ctx, settings)
		},
		OpenPlanningRuntime: func(ctx context.Context, settings Settings) (CommandApplication, error) {
			return OpenPlanningRuntime(ctx, settings)
		},
		CheckRuntime: CheckRuntime,
	}
}

func (cli *CLI) Run(ctx context.Context, arguments []string) int {
	cli.defaults()
	globals, arguments, err := parseGlobalOptions(arguments)
	if err != nil {
		return cli.writeError("github-radar", globals.JSON, Settings{}, ExitUsage, err)
	}
	if len(arguments) == 0 {
		cli.printUsage(cli.Stderr)
		return ExitUsage
	}
	if arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		cli.printUsage(cli.Stdout)
		return ExitSuccess
	}
	settings, err := cli.LoadSettings()
	if err != nil {
		return cli.writeError(arguments[0], globals.JSON, Settings{}, ExitFailure, err)
	}
	if globals.DatabasePath != "" {
		settings.DatabasePath = globals.DatabasePath
	}
	if globals.DiscoveryConfig != "" {
		settings.DiscoveryConfig = globals.DiscoveryConfig
	}

	command := arguments[0]
	args := arguments[1:]
	switch command {
	case "discover":
		return cli.runDiscover(ctx, settings, globals.JSON, args)
	case "snapshot":
		return cli.runSnapshot(ctx, settings, globals.JSON, args)
	case "import-legacy":
		return cli.runImport(ctx, settings, globals.JSON, args)
	case "topic":
		return cli.runTopic(ctx, settings, globals.JSON, args)
	case "watch":
		return cli.runWatch(ctx, settings, globals.JSON, args)
	case "export":
		return cli.runExport(ctx, settings, globals.JSON, args)
	case "doctor":
		return cli.runDoctor(settings, globals.JSON, args)
	case "run-daily":
		return cli.runDaily(ctx, settings, globals.JSON, args)
	case "serve":
		return cli.runServe(ctx, settings, globals.JSON, args)
	default:
		return cli.writeError(command, globals.JSON, settings, ExitUsage, fmt.Errorf("unknown command %q", command))
	}
}

func (cli *CLI) runDiscover(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	flags := cli.flagSet("discover")
	sourceName := flags.String("source", "all", "discovery source")
	profile := flags.String("profile", "", "GitHub Search profile")
	legacyPath := flags.String("path", "", "legacy SQLite path")
	dryRun := flags.Bool("dry-run", false, "read sources without writing")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError("discover", jsonOutput, settings, err)
	}
	sourceExplicit := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "source" {
			sourceExplicit = true
		}
	})
	if *profile != "" && !sourceExplicit {
		*sourceName = "github-search"
	}
	options := DiscoverOptions{Source: *sourceName, Profile: *profile, LegacyPath: *legacyPath, DryRun: *dryRun}
	if *sourceName == "all" {
		options.IncludeLegacy = true
		options.IncludeManual = true
	}
	return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "discover", func(application CommandApplication) (any, string, int, error) {
		report, err := application.Discover(ctx, options)
		code := ExitSuccess
		if report.Partial() {
			code = ExitPartial
		}
		human := fmt.Sprintf("Discovery found %d candidates: %d created, %d updated, %d skipped, %d failed.",
			report.CandidateCount, report.CreatedCount, report.UpdatedCount, report.SkippedCount, report.FailureCount)
		if report.DryRun {
			human = fmt.Sprintf("Dry run found %d candidates; no database changes were made.", report.CandidateCount)
		}
		human += formatOperationFailures(report.Failures)
		return report, human, code, err
	})
}

func (cli *CLI) runSnapshot(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	flags := cli.flagSet("snapshot")
	dryRun := flags.Bool("dry-run", false, "show the active target set without writing")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError("snapshot", jsonOutput, settings, err)
	}
	return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "snapshot", func(application CommandApplication) (any, string, int, error) {
		report, err := application.Snapshot(ctx, *dryRun)
		code := ExitSuccess
		if report.Partial() {
			code = ExitPartial
		}
		human := fmt.Sprintf("Snapshot %s: %d/%d succeeded, %d failed, %d skipped.", report.Date, report.SuccessCount, report.TargetCount, report.FailureCount, report.SkippedCount)
		if report.DryRun {
			human = fmt.Sprintf("Dry run: %d active repositories would be snapshotted for %s.", report.TargetCount, report.Date)
		}
		human += formatSnapshotFailures(report.Failures)
		return report, human, code, err
	})
}

func (cli *CLI) runImport(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	flags := cli.flagSet("import-legacy")
	legacyPath := flags.String("path", "", "legacy SQLite path")
	var csvPaths stringList
	flags.Var(&csvPaths, "csv", "historical CSV path (repeatable)")
	dryRun := flags.Bool("dry-run", false, "validate input without writing")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError("import-legacy", jsonOutput, settings, err)
	}
	options := ImportOptions{LegacyPath: *legacyPath, CSVPaths: append([]string(nil), csvPaths...), DryRun: *dryRun}
	return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "import-legacy", func(application CommandApplication) (any, string, int, error) {
		report, err := application.ImportLegacy(ctx, options)
		code := ExitSuccess
		if report.Partial() {
			code = ExitPartial
		}
		human := fmt.Sprintf("Imported %d candidates and %d real observations: %d inserted, %d repaired, %d protected, %d failed.",
			report.CandidateCount, report.ObservationCount, report.InsertedCount, report.RepairedCount, report.ProtectedCount, report.FailureCount)
		if report.DryRun {
			human = fmt.Sprintf("Dry run validated %d candidates and %d real observations; no database changes were made.", report.CandidateCount, report.ObservationCount)
		}
		human += formatOperationFailures(report.Failures)
		return report, human, code, err
	})
}

func (cli *CLI) runTopic(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	if len(args) == 0 {
		return cli.writeError("topic", jsonOutput, settings, ExitUsage, errors.New("topic requires list, assign, or remove"))
	}
	action := args[0]
	flags := cli.flagSet("topic " + action)
	repository := flags.String("repo", "", "repository owner/name")
	topicSlug := flags.String("topic", "", "topic slug")
	dryRun := flags.Bool("dry-run", false, "show the change without writing")
	if err := parseFlags(flags, args[1:]); err != nil {
		return cli.flagError("topic "+action, jsonOutput, settings, err)
	}
	switch action {
	case "list":
		return cli.invoke(ctx, settings, jsonOutput, "topic list", func(application CommandApplication) (any, string, int, error) {
			topics, err := application.ListTopics(ctx)
			lines := make([]string, 0, len(topics)+1)
			lines = append(lines, fmt.Sprintf("%d active topics:", len(topics)))
			for _, topic := range topics {
				lines = append(lines, fmt.Sprintf("- %s (%s)", topic.Name, topic.Slug))
			}
			return topics, strings.Join(lines, "\n"), ExitSuccess, err
		})
	case "assign", "remove":
		if *repository == "" || *topicSlug == "" {
			return cli.writeError("topic "+action, jsonOutput, settings, ExitUsage, errors.New("--repo and --topic are required"))
		}
		return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "topic "+action, func(application CommandApplication) (any, string, int, error) {
			if action == "assign" {
				result, err := application.AssignTopic(ctx, *repository, *topicSlug, *dryRun)
				human := fmt.Sprintf("Topic %s assigned to %s (changed=%t, protected=%t, dry-run=%t).", *topicSlug, *repository, result.Changed, result.Protected, *dryRun)
				return result, human, ExitSuccess, err
			}
			removed, err := application.RemoveTopic(ctx, *repository, *topicSlug, *dryRun)
			result := map[string]any{"repository": *repository, "topic": *topicSlug, "removed": removed, "dry_run": *dryRun}
			human := fmt.Sprintf("Topic %s removed from %s (removed=%t, dry-run=%t).", *topicSlug, *repository, removed, *dryRun)
			return result, human, ExitSuccess, err
		})
	default:
		return cli.writeError("topic", jsonOutput, settings, ExitUsage, fmt.Errorf("unknown topic action %q", action))
	}
}

func (cli *CLI) runWatch(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	if len(args) == 0 {
		return cli.writeError("watch", jsonOutput, settings, ExitUsage, errors.New("watch requires add, pause, or resume"))
	}
	action := args[0]
	flags := cli.flagSet("watch " + action)
	repository := flags.String("repo", "", "repository owner/name")
	note := flags.String("note", "", "manual note")
	focus := flags.Bool("focus", false, "mark as a focus repository")
	dryRun := flags.Bool("dry-run", false, "show the change without writing")
	if err := parseFlags(flags, args[1:]); err != nil {
		return cli.flagError("watch "+action, jsonOutput, settings, err)
	}
	if *repository == "" {
		return cli.writeError("watch "+action, jsonOutput, settings, ExitUsage, errors.New("--repo is required"))
	}
	switch action {
	case "add":
		return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "watch add", func(application CommandApplication) (any, string, int, error) {
			result, err := application.WatchAdd(ctx, *repository, *note, *focus, *dryRun)
			human := fmt.Sprintf("Watching %s by GitHub repository ID %d (created=%t, dry-run=%t).", result.Repository.FullName, result.Repository.GitHubRepoID, result.Created, *dryRun)
			return result, human, ExitSuccess, err
		})
	case "pause", "resume":
		status := domain.MonitoringPaused
		if action == "resume" {
			status = domain.MonitoringActive
		}
		return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "watch "+action, func(application CommandApplication) (any, string, int, error) {
			repositoryValue, err := application.WatchSet(ctx, *repository, status, *dryRun)
			human := fmt.Sprintf("Monitoring for %s is %s (dry-run=%t).", repositoryValue.FullName, status, *dryRun)
			return repositoryValue, human, ExitSuccess, err
		})
	default:
		return cli.writeError("watch", jsonOutput, settings, ExitUsage, fmt.Errorf("unknown watch action %q", action))
	}
}

func (cli *CLI) runExport(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	flags := cli.flagSet("export")
	format := flags.String("format", "", "csv, json, or sqlite")
	directory := flags.String("output", "", "export directory")
	dryRun := flags.Bool("dry-run", false, "show the target without writing")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError("export", jsonOutput, settings, err)
	}
	if *format == "" {
		return cli.writeError("export", jsonOutput, settings, ExitUsage, errors.New("--format is required"))
	}
	return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "export", func(application CommandApplication) (any, string, int, error) {
		report, err := application.Export(ctx, ExportOptions{Format: *format, Directory: *directory, DryRun: *dryRun})
		human := fmt.Sprintf("Exported %s data to %s.", report.Format, report.Path)
		if report.DryRun {
			human = fmt.Sprintf("Dry run: a %s export would be written under %s.", report.Format, report.PlannedPath)
		}
		return report, human, ExitSuccess, err
	})
}

func (cli *CLI) runDoctor(settings Settings, jsonOutput bool, args []string) int {
	if len(args) != 0 {
		return cli.writeError("doctor", jsonOutput, settings, ExitUsage, errors.New("doctor accepts no positional arguments"))
	}
	report := cli.CheckRuntime(settings)
	code := ExitSuccess
	status := "success"
	if !report.Healthy {
		code = ExitFailure
		status = "failed"
	}
	if jsonOutput {
		cli.writeJSON(outputEnvelope{Command: "doctor", Status: status, Result: report})
		return code
	}
	for _, check := range report.Checks {
		_, _ = fmt.Fprintf(cli.Stdout, "[%s] %s: %s\n", check.Status, check.Name, check.Message)
	}
	return code
}

func (cli *CLI) runDaily(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	flags := cli.flagSet("run-daily")
	dryRun := flags.Bool("dry-run", false, "show scheduled work without writing or network calls")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError("run-daily", jsonOutput, settings, err)
	}
	preflight := cli.CheckRuntime(settings)
	if !preflight.Healthy {
		report := DailyReport{
			DryRun:   *dryRun,
			Fatal:    true,
			Runtime:  preflight,
			Failures: []OperationFailure{{Stage: "runtime", Message: "runtime checks failed; target database was not opened"}},
		}
		if jsonOutput {
			cli.writeJSON(outputEnvelope{Command: "run-daily", Status: "failed", Result: report})
		} else {
			_, _ = fmt.Fprintln(cli.Stderr, "github-radar run-daily: runtime checks failed; target database was not opened")
			for _, check := range preflight.Checks {
				if check.Status == CheckFail || check.Status == CheckWarn {
					_, _ = fmt.Fprintf(cli.Stderr, "[%s] %s: %s\n", check.Status, check.Name, check.Message)
				}
			}
		}
		return ExitFailure
	}
	return cli.invokeMode(ctx, settings, *dryRun, jsonOutput, "run-daily", func(application CommandApplication) (any, string, int, error) {
		report, err := application.RunDaily(ctx, *dryRun)
		code := ExitSuccess
		if report.Fatal {
			code = ExitFailure
		} else if report.Partial() {
			code = ExitPartial
		}
		human := fmt.Sprintf("Daily run %s: %d/%d snapshots succeeded, %d failed; %d exports written.", report.RunID, report.Snapshot.SuccessCount, report.Snapshot.TargetCount, report.Snapshot.FailureCount, len(report.Exports))
		if report.DryRun {
			human = fmt.Sprintf("Dry run: %d active repositories would be snapshotted; no writes or network requests were made.", report.Snapshot.TargetCount)
		}
		if report.Runtime.DiskUsage != nil && *report.Runtime.DiskUsage >= 85 {
			human += fmt.Sprintf(" Disk usage warning: %.1f%%.", *report.Runtime.DiskUsage)
		}
		human += formatOperationFailures(report.Discovery.Failures)
		human += formatOperationFailures(report.Failures)
		human += formatSnapshotFailures(report.Snapshot.Failures)
		return report, human, code, err
	})
}

func (cli *CLI) runServe(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	flags := cli.flagSet("serve")
	address := flags.String("listen", "", "listen address")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError("serve", jsonOutput, settings, err)
	}
	listenAddress := firstNonEmpty(*address, settings.ListenAddress)
	application, err := cli.OpenRuntime(ctx, settings)
	if err != nil {
		return cli.writeError("serve", jsonOutput, settings, ExitFailure, err)
	}
	if jsonOutput {
		cli.writeJSON(outputEnvelope{Command: "serve", Status: "running", Result: map[string]string{"listen_address": listenAddress}})
	} else {
		_, _ = fmt.Fprintf(cli.Stdout, "GitHub Radar is listening on http://%s\n", listenAddress)
	}
	serveErr := application.Serve(ctx, listenAddress)
	closeErr := application.Close()
	if serveErr != nil || closeErr != nil {
		return cli.writeError("serve", jsonOutput, settings, ExitFailure, errors.Join(serveErr, closeErr))
	}
	return ExitSuccess
}

func (cli *CLI) invoke(ctx context.Context, settings Settings, jsonOutput bool, command string, action commandAction) int {
	return cli.invokeWithFactory(ctx, settings, cli.OpenRuntime, jsonOutput, command, action)
}

func (cli *CLI) invokeMode(ctx context.Context, settings Settings, dryRun, jsonOutput bool, command string, action commandAction) int {
	factory := cli.OpenRuntime
	if dryRun {
		factory = cli.OpenPlanningRuntime
	}
	return cli.invokeWithFactory(ctx, settings, factory, jsonOutput, command, action)
}

func (cli *CLI) invokeWithFactory(ctx context.Context, settings Settings, factory RuntimeFactory, jsonOutput bool, command string, action commandAction) int {
	application, err := factory(ctx, settings)
	if err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitFailure, err)
	}
	value, human, code, actionErr := action(application)
	closeErr := application.Close()
	if actionErr != nil || closeErr != nil {
		if jsonOutput {
			message := safeError(errors.Join(actionErr, closeErr), settings)
			cli.writeJSON(outputEnvelope{Command: command, Status: "failed", Result: value, Error: message})
			return ExitFailure
		}
		return cli.writeError(command, false, settings, ExitFailure, errors.Join(actionErr, closeErr))
	}
	status := "success"
	if code == ExitPartial {
		status = "partial"
	} else if code == ExitFailure {
		status = "failed"
	}
	if jsonOutput {
		cli.writeJSON(outputEnvelope{Command: command, Status: status, Result: value})
	} else if human != "" {
		_, _ = fmt.Fprintln(cli.Stdout, human)
	}
	return code
}

func (cli *CLI) flagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(cli.Stderr)
	return set
}

func parseFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return nil
}

func (cli *CLI) flagError(command string, jsonOutput bool, settings Settings, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitSuccess
	}
	return cli.writeError(command, jsonOutput, settings, ExitUsage, err)
}

func (cli *CLI) writeError(command string, jsonOutput bool, settings Settings, code int, err error) int {
	message := safeError(err, settings)
	if jsonOutput {
		status := "failed"
		if code == ExitUsage {
			status = "usage_error"
		}
		cli.writeJSON(outputEnvelope{Command: command, Status: status, Error: message})
	} else {
		_, _ = fmt.Fprintf(cli.Stderr, "github-radar %s: %s\n", command, message)
	}
	return code
}

func (cli *CLI) writeJSON(value any) {
	encoder := json.NewEncoder(cli.Stdout)
	encoder.SetEscapeHTML(true)
	_ = encoder.Encode(value)
}

func (cli *CLI) printUsage(writer io.Writer) {
	_, _ = fmt.Fprint(writer, `Usage: github-radar [--json] [--database PATH] [--config PATH] COMMAND [OPTIONS]

Commands:
  discover       Discover candidates from OSS Insight, GitHub Search, legacy, and manual inputs
  snapshot       Capture today's absolute GitHub stars for every active repository
  import-legacy  Import legacy SQLite and verified CSV history
  topic          List, assign, or remove topics
  watch          Add, pause, or resume a repository
  export         Export csv, json, or sqlite
  doctor         Check local runtime configuration and disk usage
  run-daily      Run due discovery, snapshots, and csv/json exports
  serve          Start the read-only Web dashboard

Exit codes: 0 success, 1 failure, 2 usage error, 3 partial success.
GitHub tokens are accepted only through GITHUB_RADAR_GITHUB_TOKEN.
`)
}

func (cli *CLI) defaults() {
	if cli.Stdout == nil {
		cli.Stdout = io.Discard
	}
	if cli.Stderr == nil {
		cli.Stderr = io.Discard
	}
	if cli.LoadSettings == nil {
		cli.LoadSettings = LoadSettings
	}
	if cli.OpenRuntime == nil {
		cli.OpenRuntime = func(ctx context.Context, settings Settings) (CommandApplication, error) {
			return OpenRuntime(ctx, settings)
		}
	}
	if cli.OpenPlanningRuntime == nil {
		cli.OpenPlanningRuntime = cli.OpenRuntime
	}
	if cli.CheckRuntime == nil {
		cli.CheckRuntime = CheckRuntime
	}
}

func parseGlobalOptions(args []string) (globalOptions, []string, error) {
	options := globalOptions{}
	remaining := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--json":
			options.JSON = true
		case strings.HasPrefix(argument, "--json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(argument, "--json="))
			if err != nil {
				return options, nil, fmt.Errorf("--json must be true or false")
			}
			options.JSON = value
		case argument == "--database" || argument == "--config":
			if index+1 >= len(args) {
				return options, nil, fmt.Errorf("%s requires a value", argument)
			}
			index++
			if argument == "--database" {
				options.DatabasePath = args[index]
			} else {
				options.DiscoveryConfig = args[index]
			}
		case strings.HasPrefix(argument, "--database="):
			options.DatabasePath = strings.TrimPrefix(argument, "--database=")
		case strings.HasPrefix(argument, "--config="):
			options.DiscoveryConfig = strings.TrimPrefix(argument, "--config=")
		default:
			remaining = append(remaining, argument)
		}
	}
	return options, remaining, nil
}

func safeError(err error, settings Settings) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if settings.GitHubToken != "" {
		message = strings.ReplaceAll(message, settings.GitHubToken, "[redacted]")
	}
	return message
}

func formatOperationFailures(failures []OperationFailure) string {
	var builder strings.Builder
	for _, failure := range failures {
		target := ""
		if failure.Target != "" {
			target = " " + failure.Target
		}
		_, _ = fmt.Fprintf(&builder, "\n- %s%s: %s", failure.Stage, target, failure.Message)
	}
	return builder.String()
}

func formatSnapshotFailures(failures []snapshot.Failure) string {
	var builder strings.Builder
	for _, failure := range failures {
		_, _ = fmt.Fprintf(&builder, "\n- snapshot %s (%s): %s", failure.FullName, failure.ErrorCode, failure.Message)
	}
	return builder.String()
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("path must not be empty")
	}
	*values = append(*values, value)
	return nil
}

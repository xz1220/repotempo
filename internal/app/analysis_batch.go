package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

const maxAnalysisBatchBytes = 64 << 20

type analysisBatchApplication interface {
	ImportAnalysisBatch(context.Context, []domain.AnalysisImportProject) (domain.AnalysisBatchResult, error)
}

func (runtime *Runtime) ImportAnalysisBatch(ctx context.Context, projects []domain.AnalysisImportProject) (domain.AnalysisBatchResult, error) {
	return runtime.store.ImportRepositoryAnalyses(ctx, projects)
}

func (cli *CLI) runAnalysisBatch(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	const command = "analysis import-batch"
	flags := cli.flagSet(command)
	path := flags.String("file", "", "JSON file containing 1 to 5000 projects; fills empty summaries only")
	if err := parseFlags(flags, args); err != nil {
		return cli.flagError(command, jsonOutput, settings, err)
	}
	if strings.TrimSpace(*path) == "" {
		return cli.writeError(command, jsonOutput, settings, ExitUsage, errors.New("--file is required"))
	}
	file, err := os.Open(*path)
	if err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitFailure, fmt.Errorf("read analysis batch file: %w", err))
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, maxAnalysisBatchBytes+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitFailure, fmt.Errorf("read analysis batch file: %w", err))
	}
	projects, err := decodeAnalysisBatch(contents)
	if err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitUsage, fmt.Errorf("decode analysis batch file: %w", err))
	}
	return cli.invoke(ctx, settings, jsonOutput, command, func(application CommandApplication) (any, string, int, error) {
		importer, ok := application.(analysisBatchApplication)
		if !ok {
			return nil, "", ExitFailure, errors.New("analysis batch import is not supported by this runtime")
		}
		result, err := importer.ImportAnalysisBatch(ctx, projects)
		return result, fmt.Sprintf("Imported %d project analyses; skipped %d existing summaries.", result.Imported, result.Skipped), ExitSuccess, err
	})
}

// The JSON boundary rejects null/non-string list elements instead of letting
// encoding/json silently turn null into an empty Go string.
type batchString string

func (value *batchString) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("expected a string, not null")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	*value = batchString(text)
	return nil
}

type batchStrings []string

func (values *batchStrings) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("expected an array of strings, not null")
	}
	var items []batchString
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	*values = make([]string, len(items))
	for index, item := range items {
		(*values)[index] = string(item)
	}
	return nil
}

type analysisBatchProjectFile struct {
	FullName       batchString     `json:"full_name"`
	RepositoryID   json.RawMessage `json:"repository_id"`
	SummaryZH      batchString     `json:"summary_zh"`
	KeyPoints      batchStrings    `json:"key_points"`
	UseCases       batchStrings    `json:"use_cases"`
	TechnicalNotes batchString     `json:"technical_notes"`
	Source         batchString     `json:"source"`
	Model          batchString     `json:"model"`
	AnalyzedAt     json.RawMessage `json:"analyzed_at"`
}

type analysisBatchProjectList []analysisBatchProjectFile

func (projects *analysisBatchProjectList) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return errors.New("projects must be an array")
	}
	for decoder.More() {
		if len(*projects) >= domain.MaxAnalysisBatchProjects {
			return fmt.Errorf("projects exceeds %d entries", domain.MaxAnalysisBatchProjects)
		}
		var project analysisBatchProjectFile
		if err := decoder.Decode(&project); err != nil {
			return err
		}
		*projects = append(*projects, project)
	}
	_, err = decoder.Token()
	return err
}

func decodeAnalysisBatch(contents []byte) ([]domain.AnalysisImportProject, error) {
	if len(contents) > maxAnalysisBatchBytes {
		return nil, errors.New("analysis batch file exceeds 64 MiB")
	}
	var document struct {
		Projects analysisBatchProjectList `json:"projects"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected exactly one JSON object")
	}
	if len(document.Projects) == 0 || len(document.Projects) > domain.MaxAnalysisBatchProjects {
		return nil, fmt.Errorf("projects must contain 1 to %d entries", domain.MaxAnalysisBatchProjects)
	}
	projects := make([]domain.AnalysisImportProject, 0, len(document.Projects))
	seen := make(map[string]struct{}, len(document.Projects))
	for index, input := range document.Projects {
		project := domain.AnalysisImportProject{
			FullName: strings.TrimSpace(string(input.FullName)), SummaryZH: strings.TrimSpace(string(input.SummaryZH)),
			Source: strings.TrimSpace(string(input.Source)), Model: string(input.Model),
			TechnicalNotes: string(input.TechnicalNotes), KeyPoints: []string(input.KeyPoints), UseCases: []string(input.UseCases),
		}
		if project.FullName == "" || project.SummaryZH == "" || project.Source == "" {
			return nil, fmt.Errorf("project %d requires nonempty full_name, summary_zh, and source", index+1)
		}
		name := strings.ToLower(project.FullName)
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("project %d duplicates full_name %s", index+1, project.FullName)
		}
		seen[name] = struct{}{}
		if len(input.RepositoryID) > 0 {
			var id int64
			if err := json.Unmarshal(input.RepositoryID, &id); err != nil || id <= 0 {
				return nil, fmt.Errorf("project %d repository_id must be a positive integer", index+1)
			}
			project.RepositoryID = &id
		}
		if len(input.AnalyzedAt) > 0 {
			var raw batchString
			if err := raw.UnmarshalJSON(input.AnalyzedAt); err != nil {
				return nil, fmt.Errorf("project %d analyzed_at must be an RFC3339 timestamp", index+1)
			}
			parsed, err := time.Parse(time.RFC3339Nano, string(raw))
			if err != nil || parsed.IsZero() {
				return nil, fmt.Errorf("project %d analyzed_at must be a valid RFC3339 timestamp", index+1)
			}
			project.AnalyzedAt = parsed
		}
		projects = append(projects, project)
	}
	// Typed decoding above bounds the possible nesting to this flat schema.
	// Check duplicates afterwards without accepting last-key-wins ambiguity.
	if err := rejectDuplicateBatchFields(json.NewDecoder(bytes.NewReader(contents))); err != nil {
		return nil, err
	}
	return projects, nil
}

func rejectDuplicateBatchFields(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	keys := map[string]struct{}{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name := strings.ToLower(key.(string))
			if _, duplicate := keys[name]; duplicate {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			keys[name] = struct{}{}
		}
		if err := rejectDuplicateBatchFields(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

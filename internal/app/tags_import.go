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

	"github.com/xz1220/repotempo/internal/domain"
)

func (runtime *Runtime) ImportTagsBatch(ctx context.Context, projects []domain.RepositoryTagsImport) (domain.TagsBatchResult, error) {
	return runtime.store.ImportRepositoryTags(ctx, projects)
}

type tagsImportProjectFile struct {
	RepositoryID int64           `json:"repository_id"`
	FullName     batchString     `json:"full_name"`
	GitHubTopics json.RawMessage `json:"github_topics"`
	ResearchTags json.RawMessage `json:"research_tags"`
}

type tagsImportProjectList []tagsImportProjectFile

func (projects *tagsImportProjectList) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return errors.New("projects must be an array")
	}
	for decoder.More() {
		if len(*projects) >= 10000 {
			return errors.New("tags batch exceeds 10000 projects")
		}
		var project tagsImportProjectFile
		if err := decoder.Decode(&project); err != nil {
			return err
		}
		*projects = append(*projects, project)
	}
	_, err = decoder.Token()
	return err
}

func decodeTagsImport(contents []byte) ([]domain.RepositoryTagsImport, error) {
	if len(contents) > maxAnalysisBatchBytes {
		return nil, errors.New("tags file exceeds 64 MiB")
	}
	var document struct {
		Projects tagsImportProjectList `json:"projects"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("tags file must contain one JSON object")
	}
	if len(document.Projects) == 0 {
		return nil, errors.New("tags batch requires 1 to 10000 projects")
	}
	result := make([]domain.RepositoryTagsImport, 0, len(document.Projects))
	seen := map[int64]bool{}
	for index, input := range document.Projects {
		name := strings.TrimSpace(string(input.FullName))
		if input.RepositoryID <= 0 || name == "" || seen[input.RepositoryID] {
			return nil, fmt.Errorf("tags project %d has missing or duplicate identity", index+1)
		}
		seen[input.RepositoryID] = true
		entry := domain.RepositoryTagsImport{RepositoryID: input.RepositoryID, FullName: name}
		for _, field := range []struct {
			raw    json.RawMessage
			target **[]string
		}{{input.GitHubTopics, &entry.GitHubTopics}, {input.ResearchTags, &entry.ResearchTags}} {
			if len(field.raw) == 0 {
				continue
			}
			var tags batchStrings
			if err := tags.UnmarshalJSON(field.raw); err != nil {
				return nil, fmt.Errorf("tags project %d: %w", index+1, err)
			}
			normalized, err := domain.NormalizeRepositoryTags([]string(tags))
			if err != nil {
				return nil, err
			}
			*field.target = &normalized
		}
		result = append(result, entry)
	}
	if err := rejectDuplicateBatchFields(json.NewDecoder(bytes.NewReader(contents))); err != nil {
		return nil, err
	}
	return result, nil
}

func (cli *CLI) runTagsImport(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	const command = "tags import-batch"
	if len(args) == 0 || (args[0] != "import" && args[0] != "import-batch") {
		return cli.writeError("tags", jsonOutput, settings, ExitUsage, errors.New("tags requires import-batch --file"))
	}
	flags := cli.flagSet(command)
	path := flags.String("file", "", "JSON project tags; fills unknown GitHub topics and merges research tags")
	if err := parseFlags(flags, args[1:]); err != nil {
		return cli.flagError(command, jsonOutput, settings, err)
	}
	if strings.TrimSpace(*path) == "" {
		return cli.writeError(command, jsonOutput, settings, ExitUsage, errors.New("--file is required"))
	}
	file, err := os.Open(*path)
	if err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitFailure, err)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxAnalysisBatchBytes+1))
	closeErr := file.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitFailure, err)
	}
	projects, err := decodeTagsImport(contents)
	if err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitUsage, err)
	}
	return cli.invoke(ctx, settings, jsonOutput, command, func(application CommandApplication) (any, string, int, error) {
		importer, ok := application.(interface {
			ImportTagsBatch(context.Context, []domain.RepositoryTagsImport) (domain.TagsBatchResult, error)
		})
		if !ok {
			return nil, "", ExitFailure, errors.New("tags import is not supported by this runtime")
		}
		result, err := importer.ImportTagsBatch(ctx, projects)
		return result, fmt.Sprintf("Updated tags for %d projects; skipped %d; filled %d GitHub topic lists and added %d research tags.", result.Updated, result.Skipped, result.GitHubTopicsFilled, result.ResearchTagsAdded), ExitSuccess, err
	})
}

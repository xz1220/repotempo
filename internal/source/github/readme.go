package github

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	"golang.org/x/net/html"
)

const readmeMaxRunes = 16000
const readmeMaxBytes = 1 << 20

type readmeFile struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	SHA      string `json:"sha"`
	Path     string `json:"path"`
	HTMLURL  string `json:"html_url"`
	Size     *int   `json:"size"`
}

// FetchReadme retains a bounded extract of original repository documentation.
// It performs no model calls and never executes Markdown, HTML or shell code.
func (c *Client) FetchReadme(ctx context.Context, repositoryID int64, fullName string) (domain.RepositoryReadme, error) {
	if repositoryID <= 0 || !activityFullNameValid(fullName) {
		return domain.RepositoryReadme{}, errors.New("README requires a positive repository ID and valid owner/name")
	}
	metadataURL, err := c.resolve("repositories/" + strconv.FormatInt(repositoryID, 10))
	if err != nil {
		return domain.RepositoryReadme{}, err
	}
	response, err := c.do(ctx, metadataURL, ResourceCore, nil)
	if err != nil {
		return domain.RepositoryReadme{}, err
	}
	if response.StatusCode != http.StatusOK {
		return domain.RepositoryReadme{}, apiError(response)
	}
	var metadata struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		Private  *bool  `json:"private"`
	}
	if err := json.Unmarshal(response.Body, &metadata); err != nil {
		return domain.RepositoryReadme{}, errors.New("README repository metadata is invalid JSON")
	}
	if metadata.ID != repositoryID {
		return domain.RepositoryReadme{}, &RepositoryIDMismatchError{Expected: repositoryID, Actual: metadata.ID, FullName: metadata.FullName}
	}
	if metadata.Private == nil || !activityFullNameValid(metadata.FullName) {
		return domain.RepositoryReadme{}, errors.New("README repository omitted valid identity or visibility")
	}
	if *metadata.Private {
		return domain.RepositoryReadme{}, &APIError{Code: CodeNotFoundOrPrivate, StatusCode: http.StatusForbidden, Message: "README is limited to public repositories", Path: metadataURL.Path}
	}
	// Resolve by immutable ID first so a renamed/transferred repository cannot
	// accidentally read the new owner of its former full_name.
	target, err := c.resolve("repos/" + metadata.FullName + "/readme")
	if err != nil {
		return domain.RepositoryReadme{}, err
	}
	response, err = c.do(ctx, target, ResourceCore, nil)
	if err != nil {
		return domain.RepositoryReadme{}, err
	}
	if response.StatusCode != http.StatusOK {
		return domain.RepositoryReadme{}, apiError(response)
	}
	var file readmeFile
	if err := json.Unmarshal(response.Body, &file); err != nil {
		return domain.RepositoryReadme{}, errors.New("README response is invalid JSON")
	}
	text, err := validateReadmeFile(file, metadata.FullName)
	if err != nil {
		return domain.RepositoryReadme{}, err
	}
	retained, truncated := trimReadmeRunes(text, readmeMaxRunes)
	intro, headings := extractReadme(retained)
	return domain.RepositoryReadme{
		RepositoryID: repositoryID, Intro: intro, Headings: headings, HTMLURL: file.HTMLURL,
		SHA: strings.ToLower(file.SHA), Path: file.Path, FetchedAt: c.now().UTC(),
		Truncated: truncated, Text: retained,
	}, nil
}

func validateReadmeFile(file readmeFile, fullName string) (string, error) {
	invalid := func() (string, error) {
		return "", errors.New("README response omitted valid file content or provenance")
	}
	if file.Type != "file" || file.Encoding != "base64" || file.Size == nil || *file.Size < 0 || *file.Size > readmeMaxBytes ||
		len(file.Content) > 2*readmeMaxBytes || !readmePathValid(file.Path) || !readmeHTMLURLValid(file.HTMLURL, fullName, file.Path) {
		return invalid()
	}
	if len(file.SHA) != 40 && len(file.SHA) != 64 {
		return invalid()
	}
	if _, err := hex.DecodeString(file.SHA); err != nil {
		return invalid()
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(file.Content)
	if err != nil || len(decoded) != *file.Size || !utf8.Valid(decoded) {
		return invalid()
	}
	// GitHub supplies a Git blob ID, not merely a filename checksum. Verify the
	// original complete bytes before truncating the retained text by runes.
	blob := append([]byte("blob "+strconv.Itoa(len(decoded))+"\x00"), decoded...)
	digest := ""
	if len(file.SHA) == 40 {
		sum := sha1.Sum(blob)
		digest = hex.EncodeToString(sum[:])
	} else {
		sum := sha256.Sum256(blob)
		digest = hex.EncodeToString(sum[:])
	}
	if !strings.EqualFold(digest, file.SHA) {
		return invalid()
	}
	if strings.ContainsFunc(string(decoded), func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return invalid()
	}
	return string(decoded), nil
}

func readmePathValid(path string) bool {
	if path == "" || len(path) > 1024 || !utf8.ValidString(path) || strings.Contains(path, "\\") || strings.ContainsFunc(path, unicode.IsControl) {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func readmeHTMLURLValid(raw, fullName, path string) bool {
	if len(raw) > 4096 || strings.Contains(raw, "\\") || strings.ContainsFunc(raw, unicode.IsControl) {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	if !readmePathValid(strings.TrimPrefix(parsed.Path, "/")) {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(parsed.Path, "/"), "/", 4)
	if len(parts) != 4 || !strings.EqualFold(parts[0]+"/"+parts[1], fullName) || parts[2] != "blob" {
		return false
	}
	return strings.HasSuffix(parts[3], "/"+path) && len(parts[3]) > len(path)+1
}

func trimReadmeRunes(value string, maximum int) (string, bool) {
	if utf8.RuneCountInString(value) <= maximum {
		return value, false
	}
	return string([]rune(value)[:maximum]), true
}

var readmeHeading = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
var readmeReference = regexp.MustCompile(`^\s*\[[^\]]+\]:\s*\S`)

func extractReadme(text string) (string, []string) {
	// Remove block code before stripping HTML, preserving paragraph boundaries.
	var sourceLines []string
	fence := byte(0)
	fenceLength := 0
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			length := 0
			for length < len(trimmed) && trimmed[length] == trimmed[0] {
				length++
			}
			if length >= 3 {
				if fence == 0 {
					fence, fenceLength = trimmed[0], length
					sourceLines = append(sourceLines, "")
				} else if trimmed[0] == fence && length >= fenceLength && strings.TrimSpace(trimmed[length:]) == "" {
					fence = 0
				}
				continue
			}
		}
		if fence != 0 || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") || readmeReference.MatchString(line) {
			continue
		}
		sourceLines = append(sourceLines, line)
	}
	plain := readmeWithoutHTML(strings.Join(sourceLines, "\n"))
	headings := make([]string, 0, 8)
	var paragraph []string
	intro := ""
	finishParagraph := func() {
		if intro == "" && len(paragraph) > 0 {
			intro, _ = trimReadmeRunes(strings.Join(paragraph, " "), 1200)
		}
		paragraph = nil
	}
	lines := strings.Split(plain, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			finishParagraph()
			continue
		}
		if matches := readmeHeading.FindStringSubmatch(trimmed); matches != nil {
			finishParagraph()
			label, _ := trimReadmeRunes(readmeInlineText(matches[1]), 160)
			if label != "" && len(headings) < 8 {
				headings = append(headings, label)
			}
			continue
		}
		if index+1 < len(lines) && isReadmeUnderline(strings.TrimSpace(lines[index+1])) {
			finishParagraph()
			label, _ := trimReadmeRunes(readmeInlineText(trimmed), 160)
			if label != "" && len(headings) < 8 {
				headings = append(headings, label)
			}
			continue
		}
		if isReadmeUnderline(trimmed) || strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			finishParagraph()
			continue
		}
		value := readmeInlineText(strings.TrimLeft(trimmed, "> "))
		if value != "" && intro == "" {
			paragraph = append(paragraph, value)
		}
	}
	finishParagraph()
	return intro, headings
}

func isReadmeUnderline(value string) bool {
	if len(value) < 3 || value[0] != '=' && value[0] != '-' {
		return false
	}
	return strings.Trim(value, string(value[0])) == ""
}

func readmeWithoutHTML(source string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	var text strings.Builder
	blocked := ""
	for {
		typeOfToken := tokenizer.Next()
		if typeOfToken == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			return text.String()
		}
		token := tokenizer.Token()
		if blocked != "" {
			if typeOfToken == html.EndTagToken && token.Data == blocked {
				blocked = ""
				text.WriteByte('\n')
			}
			continue
		}
		if typeOfToken == html.StartTagToken {
			switch token.Data {
			case "script", "style", "pre", "iframe", "object", "svg", "math", "picture":
				blocked = token.Data
				continue
			}
		}
		if typeOfToken == html.TextToken {
			text.WriteString(token.Data)
			continue
		}
		if typeOfToken == html.StartTagToken || typeOfToken == html.EndTagToken || typeOfToken == html.SelfClosingTagToken {
			switch token.Data {
			case "p", "div", "section", "br", "hr", "ul", "ol", "li", "table", "tr":
				text.WriteByte('\n')
			case "h1", "h2", "h3", "h4", "h5", "h6":
				text.WriteByte('\n')
				if typeOfToken == html.StartTagToken {
					text.WriteString(strings.Repeat("#", int(token.Data[1]-'0')) + " ")
				}
			}
		}
	}
	return text.String()
}

func readmeInlineText(value string) string {
	var text strings.Builder
	for index := 0; index < len(value); {
		start := index
		image := value[index] == '!' && index+1 < len(value) && value[index+1] == '['
		if image {
			start++
		}
		if value[start] == '[' {
			end := readmeBalanced(value, start, '[', ']')
			if end >= 0 && end+1 < len(value) && (value[end+1] == '(' || value[end+1] == '[') {
				open, close := value[end+1], byte(')')
				if open == '[' {
					close = ']'
				}
				last := readmeBalanced(value, end+1, open, close)
				if last >= 0 {
					if !image {
						text.WriteString(readmeInlineText(value[start+1 : end]))
					}
					index = last + 1
					continue
				}
			}
		}
		if value[index] != '`' {
			text.WriteByte(value[index])
		}
		index++
	}
	result := strings.NewReplacer("**", "", "__", "", "~~", "").Replace(text.String())
	return strings.Join(strings.Fields(result), " ")
}

func readmeBalanced(value string, start int, open, close byte) int {
	depth := 0
	for index := start; index < len(value); index++ {
		if value[index] == '\\' {
			index++
			continue
		}
		if value[index] == open {
			depth++
		}
		if value[index] == close {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

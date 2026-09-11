package integrations_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/integrations"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

func portablePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// No Node, Python, jq or package manager exists in the install/client PATH.
	for _, name := range strings.Fields("sh curl openssl tar gzip mktemp chmod mkdir mv rm rmdir od awk grep date wc tr cat ls id dirname uname") {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("portable Skill requires system utility %s", name)
		}
		if err := os.Symlink(path, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func portableEnv(path string, values ...string) []string {
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "REPOTEMPO_") && !strings.HasPrefix(value, "PATH=") {
			env = append(env, value)
		}
	}
	return append(append(env, "PATH="+path), values...)
}

type skillQuery struct{ clientQuery }

func (q skillQuery) GetRepositoryDetail(ctx context.Context, id int64, _ time.Time) (web.RepositoryDetail, error) {
	principal, ok := domain.PrincipalFromContext(ctx)
	if !ok || principal.UserID != 42 || principal.Admin || id != 42 {
		return web.RepositoryDetail{}, web.ErrInvalid
	}
	return web.RepositoryDetail{Repository: web.RepositoryMetric{ID: id, FullName: "fixture/project"}}, nil
}

func TestPortableSkillInstallsWithoutNodeAndUsesRealGoAPI(t *testing.T) {
	path := portablePath(t)
	ctx := context.Background()
	db, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "skill.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte("portable skill fixture session"))
	tokenHash := hex.EncodeToString(hash[:])
	if err := db.PutAuthSession(ctx, tokenHash, domain.AuthSession{GitHubUserID: 42, Login: "skill-owner", CSRFToken: tokenHash, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	service, err := agentaccess.New(db, agentaccess.Options{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := service.Create(ctx, 42, "portable fixture", nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	queryer := skillQuery{clientQuery{seen: make(chan clientQueryObservation, 4)}}
	handler, err := web.New(queryer, web.Options{AgentAccess: service})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := integrations.SkillArchive()
	if err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/integrations/repotempo-skill.tar.gz" {
			downloads.Add(1)
			if r.URL.RawQuery != "" || r.Header.Get("X-RepoTempo-Key") != "" || r.Header.Get("Authorization") != "" {
				t.Error("public Skill download included credentials")
			}
			_, _ = w.Write(archive)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	target := filepath.Join(t.TempDir(), "space in path", "repotempo")
	if runtime.GOOS == "darwin" {
		// Real inherited ACLs must not follow the private staging directory or
		// credential into the installation; the user's parent ACL stays intact.
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(filepath.Join(path, "chmod"), "+a", "everyone allow read,execute,file_inherit,directory_inherit", filepath.Dir(target))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("cannot prepare isolated inherited ACL fixture: %v: %s", err, output)
		}
	}
	installer, _ := filepath.Abs("install-skill.sh")
	install := func() ([]byte, error) {
		command := exec.Command(filepath.Join(path, "sh"), installer)
		command.Env = portableEnv(path, "REPOTEMPO_URL="+server.URL, "REPOTEMPO_AK="+key.Key.ID, "REPOTEMPO_SK="+key.Secret, "REPOTEMPO_SKILL_DIR="+target)
		return command.CombinedOutput()
	}
	output, err := install()
	if err != nil {
		t.Fatalf("install without Node/Python failed: %v: %s", err, output)
	}
	if strings.Contains(string(output), key.Secret) || downloads.Load() != 1 {
		t.Fatal("installer exposed a secret or failed to fetch one public bundle")
	}
	if runtime.GOOS == "darwin" {
		command := exec.Command(filepath.Join(path, "ls"), "-lde", filepath.Dir(target))
		if output, err := command.CombinedOutput(); err != nil || !bytes.Contains(output, []byte("everyone")) {
			t.Fatal("installer altered the unrelated parent directory ACL")
		}
	}
	credentials, err := os.ReadFile(filepath.Join(target, ".credentials"))
	if err != nil || string(credentials) != server.URL+"\n"+key.Key.ID+"\n"+key.Secret+"\n" {
		t.Fatal("installer did not preserve the exact dynamic credential")
	}
	for filename, mode := range map[string]os.FileMode{".": 0700, "scripts": 0700, ".credentials": 0600, "SKILL.md": 0600} {
		info, err := os.Stat(filepath.Join(target, filename))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("unsafe installed permissions for %s", filename)
		}
	}
	call := func(args ...string) ([]byte, []byte, error) {
		requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		command := exec.CommandContext(requestContext, filepath.Join(path, "sh"), append([]string{filepath.Join(target, "scripts", "repotempo.sh")}, args...)...)
		command.Env = portableEnv(path)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if bytes.Contains(stderr.Bytes(), []byte(key.Secret)) || bytes.Contains(output, []byte(key.Secret)) {
			t.Fatal("client exposed secret")
		}
		return output, stderr.Bytes(), err
	}
	output, stderr, err := call("account")
	if err != nil {
		t.Fatalf("portable HMAC failed against Go: %v: %s", err, stderr)
	}
	var account struct {
		User struct {
			ID    int64  `json:"github_user_id"`
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.Unmarshal(output, &account); err != nil || account.User.ID != 42 || account.User.Login != "skill-owner" {
		t.Fatal("portable client received the wrong account")
	}
	if _, stderr, err := call("search", "中文 + & agent", "--page", "2", "--size", "6", "--period", "7d", "--sort", "growth_rate"); err != nil {
		t.Fatalf("UTF-8 signed query failed: %v: %s", err, stderr)
	}
	observed := <-queryer.seen
	if observed.query.Search != "中文 + & agent" || observed.query.Offset != 6 || observed.query.Limit != 6 || observed.query.OnlyFocus || observed.principal.UserID != 42 || observed.principal.Admin {
		t.Fatal("portable query lost exact encoding, pagination or account scope")
	}
	if _, stderr, err := call("watchlist"); err != nil {
		t.Fatalf("portable watchlist failed: %v: %s", err, stderr)
	}
	observed = <-queryer.seen
	if !observed.query.OnlyFocus || observed.principal.UserID != 42 || observed.principal.Admin {
		t.Fatal("portable watchlist lost account scope")
	}
	if output, stderr, err := call("repository", "42"); err != nil || !bytes.Contains(output, []byte(`"full_name":"fixture/project"`)) {
		t.Fatalf("portable repository detail failed: %v: %s", err, stderr)
	}
	if err := os.WriteFile(filepath.Join(target, ".credentials"), append(bytes.Clone(credentials), []byte("unterminated extra data")...), 0600); err != nil {
		t.Fatal(err)
	}
	if output, stderr, err := call("account"); err == nil || len(output) != 0 || !bytes.Contains(stderr, []byte("Invalid local credential file")) {
		t.Fatal("client accepted trailing data in a credential file")
	}
	if err := os.WriteFile(filepath.Join(target, ".credentials"), credentials, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(target, ".credentials"), 0644); err != nil {
		t.Fatal(err)
	}
	if output, stderr, err := call("account"); err == nil || len(output) != 0 || !bytes.Contains(stderr, []byte("permission 600")) {
		t.Fatal("client accepted a credential file readable by other users")
	}
	if err := os.Chmod(filepath.Join(target, ".credentials"), 0600); err != nil {
		t.Fatal(err)
	}
	// A managed reinstall rotates its own files, while preserving user additions.
	sentinel := filepath.Join(target, "personal-notes.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	previousKey := key.Key.ID
	key, err = service.Create(ctx, 42, "rotated portable fixture", nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := install(); err != nil {
		t.Fatalf("managed reinstall failed: %v: %s", err, output)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatal("reinstall changed an unrelated file")
	}
	if err := service.Revoke(ctx, 42, previousKey); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := call("account"); err != nil {
		t.Fatalf("reinstall did not rotate the local credential: %v: %s", err, stderr)
	}
	if err := service.Revoke(ctx, 42, key.Key.ID); err != nil {
		t.Fatal(err)
	}
	if output, stderr, err := call("account"); err == nil || len(output) != 0 || !bytes.Contains(stderr, []byte("Authentication failed")) {
		t.Fatal("portable client did not reject a revoked key with a safe error")
	}
}

func TestPortableSkillRejectsRedirectsAndUnsafeInstallTargets(t *testing.T) {
	path := portablePath(t)
	var destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationHits.Add(1)
		_, _ = w.Write([]byte("unexpected"))
	}))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirect.Close()
	ak := "rt_ak_" + strings.Repeat("a", 32)
	sk := "rt_sk_" + strings.Repeat("b", 64)
	installer, _ := filepath.Abs("install-skill.sh")
	client, _ := filepath.Abs("repotempo-skill/scripts/repotempo.sh")
	for _, file := range []string{installer, client} {
		args := []string{file}
		if file == client {
			args = append(args, "account")
		}
		command := exec.Command(filepath.Join(path, "sh"), args...)
		command.Env = portableEnv(path, "REPOTEMPO_URL="+redirect.URL, "REPOTEMPO_AK="+ak, "REPOTEMPO_SK="+sk, "REPOTEMPO_SKILL_DIR="+filepath.Join(t.TempDir(), "repotempo"))
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(strings.ToLower(string(output)), "redirect") || bytes.Contains(output, []byte(sk)) {
			t.Fatalf("redirect was not safely rejected for %s", filepath.Base(file))
		}
	}
	if destinationHits.Load() != 0 {
		t.Fatal("credential-bearing client or installer followed a redirect")
	}
	archive, _ := integrations.SkillArchive()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	defer server.Close()
	for _, kind := range []string{"unmanaged", "symlink", "multiline-secret", "userinfo-origin", "remote-http"} {
		t.Run(kind, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "repotempo")
			origin, secret := server.URL, sk
			switch kind {
			case "unmanaged":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(t.TempDir(), target); err != nil {
					t.Fatal(err)
				}
			case "multiline-secret":
				secret += "\nmalformed"
			case "userinfo-origin":
				origin = "https://user:password@example.com"
			case "remote-http":
				origin = "http://example.com"
			}
			command := exec.Command(filepath.Join(path, "sh"), installer)
			command.Env = portableEnv(path, "REPOTEMPO_URL="+origin, "REPOTEMPO_AK="+ak, "REPOTEMPO_SK="+secret, "REPOTEMPO_SKILL_DIR="+target)
			output, err := command.CombinedOutput()
			if err == nil || bytes.Contains(output, []byte(sk)) {
				t.Fatal("unsafe installation was not refused safely")
			}
			if _, err := os.Stat(filepath.Join(target, ".credentials")); !os.IsNotExist(err) {
				t.Fatal("failed installer changed target credentials")
			}
		})
	}
}

func TestPortableSkillArchiveContainsOnlyRuntimeIndependentFiles(t *testing.T) {
	archive, err := integrations.SkillArchive()
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	expected := map[string]bool{"repotempo/SKILL.md": true, "repotempo/scripts/lib.sh": true, "repotempo/scripts/repotempo.sh": true}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !expected[header.Name] || header.Typeflag != tar.TypeReg || header.Mode != 0600 {
			t.Fatalf("unexpected Skill archive member: %s", header.Name)
		}
		delete(expected, header.Name)
	}
	if len(expected) != 0 {
		t.Fatal("portable Skill archive is incomplete")
	}
	installer, err := integrations.SkillInstaller()
	if err != nil || !bytes.HasPrefix(installer, []byte("#!/bin/sh\n")) {
		t.Fatal("public installer missing")
	}
}

func TestPortableSkillCredentialMetadataIndicators(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{}}`))
	}))
	defer server.Close()
	for _, indicator := range []string{"", "@", ".", "+", "?", "hidden_acl"} {
		t.Run("suffix_"+indicator, func(t *testing.T) {
			path := portablePath(t)
			target := filepath.Join(t.TempDir(), "repotempo")
			if err := os.MkdirAll(filepath.Join(target, "scripts"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"lib.sh", "repotempo.sh"} {
				data, err := os.ReadFile(filepath.Join("repotempo-skill", "scripts", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "scripts", name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			credentials := server.URL + "\nrt_ak_" + strings.Repeat("a", 32) + "\nrt_sk_" + strings.Repeat("b", 64) + "\n"
			if err := os.WriteFile(filepath.Join(target, ".credentials"), []byte(credentials), 0600); err != nil {
				t.Fatal(err)
			}
			// Simulate BSD xattrs, GNU security context, ACL and unknown metadata
			// independently of the filesystem used by the test runner.
			if err := os.Remove(filepath.Join(path, "ls")); err != nil {
				t.Fatal(err)
			}
			suffix := indicator
			if indicator == "hidden_acl" {
				suffix = "@"
			}
			wrapper := "#!/bin/sh\nprintf '%s\\n' '-rw-------" + suffix + " 1 " + fmt.Sprint(os.Getuid()) + " 20 100 Sep 11 00:00 .credentials'\n"
			if indicator == "hidden_acl" {
				wrapper += "[ \"$1\" != -nde ] || printf '%s\\n' ' 0: group:everyone allow read'\n"
			}
			if err := os.WriteFile(filepath.Join(path, "ls"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			// Exercise the Darwin-only detailed ACL check on every host.
			if err := os.Remove(filepath.Join(path, "uname")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "uname"), []byte("#!/bin/sh\nprintf 'Darwin\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(filepath.Join(path, "sh"), filepath.Join(target, "scripts", "repotempo.sh"), "account")
			command.Env = portableEnv(path)
			output, err := command.CombinedOutput()
			if indicator == "+" || indicator == "?" || indicator == "hidden_acl" {
				if err == nil || !(bytes.Contains(output, []byte("permission 600")) || bytes.Contains(output, []byte("extended ACL"))) {
					t.Fatal("client did not reject ACL or unknown access metadata")
				}
			} else if err != nil {
				t.Fatalf("client rejected safe platform metadata %q: %v: %s", indicator, err, output)
			}
		})
	}
}

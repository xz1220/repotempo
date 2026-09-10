package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
	loginauth "github.com/xz1220/repotempo/internal/service/auth"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

// Explicit local-only browser fixture, compiled into tests only. It never uses
// deployment credentials, existing databases, or a production identity provider.
func TestBrowserAccountFixture(t *testing.T) {
	if os.Getenv("REPOTEMPO_BROWSER_FIXTURE") != "1" {
		t.Skip("explicit browser QA fixture")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8879")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	store, err := sqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, err := loginauth.New(loginauth.Configuration{ClientID: "browser-fixture", ClientSecret: "browser-fixture", PublicURL: "http://127.0.0.1:8879", AllowedUserIDs: []int64{42}, AllowPublicSignup: true}, store, loginauth.Options{})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := agentaccess.New(store, agentaccess.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(populatedFake(), Options{Auth: auth, AgentAccess: keys, Locale: localeChinese})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture/sign-in", func(w http.ResponseWriter, r *http.Request) {
		var token, csrf [32]byte
		_, _ = rand.Read(token[:])
		_, _ = rand.Read(csrf[:])
		raw := hex.EncodeToString(token[:])
		hash := sha256.Sum256([]byte(raw))
		now := time.Now()
		if err := store.PutAuthSession(r.Context(), hex.EncodeToString(hash[:]), domain.AuthSession{GitHubUserID: 99, Login: "preview-user", CSRFToken: hex.EncodeToString(csrf[:]), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			http.Error(w, "fixture unavailable", 500)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "repotempo_session", Value: raw, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/repositories?view=all&lang=zh-CN", http.StatusSeeOther)
	})
	mux.Handle("/", h)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	t.Log("Local fixture ready at http://127.0.0.1:8879/fixture/sign-in")
	<-time.After(10 * time.Minute)
	_ = server.Close()
}

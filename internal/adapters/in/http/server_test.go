package httpserver_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpserver "github.com/sploitzberg/go-hexagonal-architecture-template/internal/adapters/in/http"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/adapters/out/memory"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/app"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	store := memory.NewStore()
	svc := app.New(store)
	return httpserver.NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
}

func TestHealth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHash(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	body := strings.NewReader(`{"message":"hello"}`)
	resp, err := http.Post(srv.URL+"/v1/hash", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if out.SHA256 != want {
		t.Fatalf("got %q, want %q", out.SHA256, want)
	}
}

func TestHash_emptyMessage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/hash", "application/json", strings.NewReader(`{"message":""}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestHash_invalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/hash", "application/json", strings.NewReader(`{`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestStore_created(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/store", "application/json", strings.NewReader(`{"text":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", resp.StatusCode)
	}
}

func TestStore_emptyText(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/v1/store", "application/json", strings.NewReader(`{"text":""}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestStoreAndMessages(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestHandler(t))
	t.Cleanup(srv.Close)

	post := func(jsonBody string) {
		t.Helper()
		resp, err := http.Post(srv.URL+"/v1/store", "application/json", strings.NewReader(jsonBody))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	post(`{"text":"alpha"}`)
	post(`{"text":"beta"}`)

	resp, err := http.Get(srv.URL + "/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out struct {
		Messages []string `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 2 || out.Messages[0] != "alpha" || out.Messages[1] != "beta" {
		t.Fatalf("messages = %#v", out.Messages)
	}
}

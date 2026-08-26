package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

var testToken = []byte("0123456789abcdef0123456789abcdef")

func TestRemoteAccessRequiresAuthentication(t *testing.T) {
	t.Parallel()
	_, _, handler := openTestAPI(t, 10)
	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("response = %d, headers=%v", response.Code, response.Header())
	}
}

func TestRemoteAccessPutAndGetThroughSingleOwner(t *testing.T) {
	t.Parallel()
	path, _, handler := openTestAPI(t, 10)
	body := `{"q":2,"r":-1,"content":"bounded remote cell","tags":["remote"],"confidence":0.8}`
	put := authorizedRequest(http.MethodPut, "/v1/cells", strings.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusNoContent {
		t.Fatalf("put = %d: %s", putResponse.Code, putResponse.Body.String())
	}

	get := authorizedRequest(http.MethodGet, "/v1/cells?q=2&r=-1", http.NoBody)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", getResponse.Code, getResponse.Body.String())
	}
	var got cellResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Content != "bounded remote cell" || got.SourceID != "remote-access" || got.Confidence != 0.8 {
		t.Fatalf("cell = %#v", got)
	}

	if _, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: bytes.Repeat([]byte{0x42}, 32)}); !errors.Is(err, hexxladb.ErrDatabaseLocked) {
		t.Fatalf("second owner error = %v, want ErrDatabaseLocked", err)
	}
}

func TestRemoteAccessRejectsUnknownJSONAndRateOverflow(t *testing.T) {
	t.Parallel()
	_, _, handler := openTestAPI(t, 2)
	for i, want := range []int{http.StatusBadRequest, http.StatusOK, http.StatusTooManyRequests} {
		var request *http.Request
		if i == 0 {
			request = authorizedRequest(http.MethodPut, "/v1/cells", strings.NewReader(`{"q":0,"r":0,"content":"x","confidence":1,"unknown":true}`))
			request.Header.Set("Content-Type", "application/json")
		} else {
			request = authorizedRequest(http.MethodGet, "/healthz", http.NoBody)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("request %d = %d, want %d: %s", i, response.Code, want, response.Body.String())
		}
	}
}

func TestRemoteAccessRejectsAmbiguousQueryAndLookalikeMediaType(t *testing.T) {
	t.Parallel()
	_, _, handler := openTestAPI(t, 10)

	get := authorizedRequest(http.MethodGet, "/v1/cells?q=1&q=2&r=3", http.NoBody)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous query = %d, want %d", getResponse.Code, http.StatusBadRequest)
	}

	put := authorizedRequest(http.MethodPut, "/v1/cells", strings.NewReader(`{"q":0,"r":0,"content":"x","confidence":1}`))
	put.Header.Set("Content-Type", "application/json-malformed")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("lookalike media type = %d, want %d", putResponse.Code, http.StatusUnsupportedMediaType)
	}
}

func TestRemoteAccessConfigurationIsFailClosed(t *testing.T) {
	t.Parallel()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	values := map[string]string{
		"HEXXLA_DB_PATH":        "remote.db",
		"HEXXLA_REMOTE_TOKEN":   string(testToken),
		"HEXXLA_ENCRYPTION_KEY": key,
	}
	getenv := func(name string) string { return values[name] }
	cfg, err := loadConfig(getenv)
	if err != nil || cfg.address != defaultListenAddress {
		t.Fatalf("config = %#v, %v", cfg, err)
	}
	values["HEXXLA_REMOTE_ADDR"] = "0.0.0.0:8080"
	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("non-loopback address was accepted")
	}
	values["HEXXLA_REMOTE_ADDR"] = "localhost:8080"
	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("non-numeric listen address was accepted")
	}
	values["HEXXLA_REMOTE_ADDR"] = defaultListenAddress
	values["HEXXLA_REMOTE_TOKEN"] = "short"
	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("short token was accepted")
	}
}

func TestFixedWindowLimiterResets(t *testing.T) {
	t.Parallel()
	limiter := newFixedWindowLimiter(1, time.Minute)
	now := time.Unix(1, 0)
	if !limiter.allow(now) || limiter.allow(now.Add(time.Second)) || !limiter.allow(now.Add(time.Minute)) {
		t.Fatal("unexpected fixed-window decisions")
	}
}

func openTestAPI(t *testing.T, requestLimit int) (string, *hexxladb.DB, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "remote.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: bytes.Repeat([]byte{0x42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return path, db, newCellAPI(db, testToken, requestLimit, 2, logger).handler()
}

func authorizedRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+string(testToken))
	return request
}

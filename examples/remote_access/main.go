package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hexxla/hexxladb"
)

const (
	defaultListenAddress = "127.0.0.1:8080"
	maxRequestBody       = 64 << 10
)

type config struct {
	address           string
	databasePath      string
	bearerToken       []byte
	encryptionKey     []byte
	requestsPerMinute int
	maxConcurrent     int
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	os.Exit(run(logger))
}

func run(logger *slog.Logger) int {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return 2
	}
	defer clearConfigSecrets(&cfg)
	db, err := hexxladb.Open(cfg.databasePath, &hexxladb.Options{EncryptionKey: cfg.encryptionKey})
	if err != nil {
		logger.Error("open database", "error", err)
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()

	listener, err := net.Listen("tcp", cfg.address)
	if err != nil {
		logger.Error("listen", "error", err)
		return 1
	}
	api := newCellAPI(db, cfg.bearerToken, cfg.requestsPerMinute, cfg.maxConcurrent, logger)
	clearConfigSecrets(&cfg)
	server := &http.Server{
		Handler:           api.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	shutdownDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(shutdownCtx)
	}()
	logger.Info("remote access example listening", "address", listener.Addr().String())
	serveErr := server.Serve(listener)
	stop()
	if err := <-shutdownDone; err != nil {
		logger.Error("shutdown", "error", err)
		return 1
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Error("serve", "error", serveErr)
		return 1
	}
	return 0
}

func clearConfigSecrets(cfg *config) {
	clear(cfg.bearerToken)
	clear(cfg.encryptionKey)
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		address:           strings.TrimSpace(getenv("HEXXLA_REMOTE_ADDR")),
		databasePath:      strings.TrimSpace(getenv("HEXXLA_DB_PATH")),
		bearerToken:       []byte(getenv("HEXXLA_REMOTE_TOKEN")),
		requestsPerMinute: 120,
		maxConcurrent:     16,
	}
	if cfg.address == "" {
		cfg.address = defaultListenAddress
	}
	if err := validateLoopbackAddress(cfg.address); err != nil {
		return config{}, err
	}
	if cfg.databasePath == "" {
		return config{}, errors.New("HEXXLA_DB_PATH is required")
	}
	if len(cfg.bearerToken) < 32 {
		return config{}, errors.New("HEXXLA_REMOTE_TOKEN must contain at least 32 bytes")
	}
	key, err := base64.StdEncoding.DecodeString(getenv("HEXXLA_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return config{}, errors.New("HEXXLA_ENCRYPTION_KEY must be a standard-base64 32-byte key")
	}
	cfg.encryptionKey = key
	return cfg, nil
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("HEXXLA_REMOTE_ADDR: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("HEXXLA_REMOTE_ADDR must use a numeric loopback address; terminate TLS at a trusted local proxy")
	}
	return nil
}

type cellAPI struct {
	db      *hexxladb.DB
	token   []byte
	limiter *fixedWindowLimiter
	slots   chan struct{}
	logger  *slog.Logger
}

func newCellAPI(db *hexxladb.DB, token []byte, requestsPerMinute, maxConcurrent int, logger *slog.Logger) *cellAPI {
	return &cellAPI{
		db:      db,
		token:   append([]byte(nil), token...),
		limiter: newFixedWindowLimiter(requestsPerMinute, time.Minute),
		slots:   make(chan struct{}, maxConcurrent),
		logger:  logger,
	}
}

func (a *cellAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /v1/cells", a.getCell)
	mux.HandleFunc("PUT /v1/cells", a.putCell)
	return a.secure(mux)
}

func (a *cellAPI) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		select {
		case a.slots <- struct{}{}:
			defer func() { <-a.slots }()
		default:
			writeError(w, http.StatusServiceUnavailable, "request capacity exhausted")
			return
		}
		if !a.authenticated(r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !a.limiter.allow(time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *cellAPI) authenticated(header string) bool {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || len(token) != len(a.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), a.token) == 1
}

func (a *cellAPI) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type putCellRequest struct {
	Q          int      `json:"q"`
	R          int      `json:"r"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	Confidence float64  `json:"confidence"`
}

func (a *cellAPI) putCell(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var request putCellRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.Content == "" || math.IsNaN(request.Confidence) || math.IsInf(request.Confidence, 0) || request.Confidence < 0 || request.Confidence > 1 {
		writeError(w, http.StatusBadRequest, "invalid cell")
		return
	}
	packed, err := hexxladb.Pack(hexxladb.Coord{Q: request.Q, R: request.R})
	if err != nil {
		writeError(w, http.StatusBadRequest, "coordinate out of range")
		return
	}
	now := time.Now().UTC().UnixNano()
	err = a.db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(r.Context(), hexxladb.CellRecord{
			Key: packed, RawContent: request.Content, Tags: request.Tags,
			Provenance: hexxladb.ProvenanceWire{
				SourceID: "remote-access", Confidence: request.Confidence,
				CreatedAt: now, UpdatedAt: now,
			},
		})
	})
	if err != nil {
		a.logger.Error("put cell failed", "error", err)
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	w.Header().Del("Content-Type")
	w.WriteHeader(http.StatusNoContent)
}

type cellResponse struct {
	Q          int      `json:"q"`
	R          int      `json:"r"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	SourceID   string   `json:"source_id"`
	Confidence float64  `json:"confidence"`
}

func (a *cellAPI) getCell(w http.ResponseWriter, r *http.Request) {
	coord, err := queryCoord(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid coordinate")
		return
	}
	packed, err := hexxladb.Pack(coord)
	if err != nil {
		writeError(w, http.StatusBadRequest, "coordinate out of range")
		return
	}
	var cell hexxladb.CellRecord
	var found bool
	if err := a.db.View(func(tx *hexxladb.Tx) error {
		var getErr error
		cell, found, getErr = tx.GetCell(packed)
		return getErr
	}); err != nil {
		a.logger.Error("get cell failed", "error", err)
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}
	writeJSON(w, http.StatusOK, cellResponse{
		Q: coord.Q, R: coord.R, Content: cell.RawContent, Tags: cell.Tags,
		SourceID: cell.Provenance.SourceID, Confidence: cell.Provenance.Confidence,
	})
}

func queryCoord(r *http.Request) (hexxladb.Coord, error) {
	query := r.URL.Query()
	if len(query) != 2 || len(query["q"]) != 1 || len(query["r"]) != 1 {
		return hexxladb.Coord{}, errors.New("exactly one q and r value are required")
	}
	q, err := strconv.ParseInt(query["q"][0], 10, 32)
	if err != nil {
		return hexxladb.Coord{}, err
	}
	rValue, err := strconv.ParseInt(query["r"][0], 10, 32)
	if err != nil {
		return hexxladb.Coord{}, err
	}
	return hexxladb.Coord{Q: int(q), R: int(rValue)}, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type fixedWindowLimiter struct {
	mu          sync.Mutex
	started     time.Time
	count       int
	limit       int
	windowWidth time.Duration
}

func newFixedWindowLimiter(limit int, width time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, windowWidth: width}
}

func (l *fixedWindowLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started.IsZero() || now.Sub(l.started) >= l.windowWidth {
		l.started, l.count = now, 0
	}
	if l.count >= l.limit {
		return false
	}
	l.count++
	return true
}

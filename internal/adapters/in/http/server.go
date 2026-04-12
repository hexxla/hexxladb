// Package httpserver is the primary (inbound) HTTP adapter.
package httpserver

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/app"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain"
)

// Handler serves HTTP routes for the demo API.
type Handler struct {
	svc     *app.Service
	log     *slog.Logger
	maxBody int64
	mux     *http.ServeMux
}

// NewHandler builds the HTTP handler tree with logging middleware.
func NewHandler(svc *app.Service, log *slog.Logger, maxBodyBytes int64) http.Handler {
	h := &Handler{
		svc:     svc,
		log:     log,
		maxBody: maxBodyBytes,
		mux:     http.NewServeMux(),
	}
	h.mux.HandleFunc("GET /health", h.handleHealth)
	h.mux.HandleFunc("POST /v1/hash", h.handleHash)
	h.mux.HandleFunc("POST /v1/store", h.handleStore)
	h.mux.HandleFunc("GET /v1/messages", h.handleMessages)
	return h.loggingMiddleware(h.mux)
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type hashRequest struct {
	Message string `json:"message"`
}

type hashResponse struct {
	SHA256 string `json:"sha256"`
}

func (h *Handler) handleHash(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, h.maxBody)
	defer func() { _ = body.Close() }()

	var req hashRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	sha, err := h.svc.HashMessage(r.Context(), req.Message)
	if errors.Is(err, domain.ErrInvalidInput) {
		writeJSONError(w, http.StatusBadRequest, "message is required")
		return
	}
	if errors.Is(err, domain.ErrContentTooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "message too large")
		return
	}
	if err != nil {
		h.log.Error("hash", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(hashResponse{SHA256: sha})
}

type storeRequest struct {
	Text string `json:"text"`
}

func (h *Handler) handleStore(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, h.maxBody)
	defer func() { _ = body.Close() }()

	var req storeRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := h.svc.StoreText(r.Context(), req.Text); err != nil {
		if errors.Is(err, domain.ErrInvalidInput) {
			writeJSONError(w, http.StatusBadRequest, "text is required")
			return
		}
		if errors.Is(err, domain.ErrContentTooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "text too large")
			return
		}
		h.log.Error("store", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

type messagesResponse struct {
	Messages []string `json:"messages"`
}

func (h *Handler) handleMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.svc.ListMessages(r.Context())
	if err != nil {
		h.log.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(messagesResponse{Messages: msgs})
}

type jsonError struct {
	Error string `json:"error"`
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(jsonError{Error: msg})
}

// captureStatus wraps [http.ResponseWriter] to record the status code for logging.
type captureStatus struct {
	http.ResponseWriter
	code int
}

func (c *captureStatus) WriteHeader(code int) {
	c.code = code
	c.ResponseWriter.WriteHeader(code)
}

func (h *Handler) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		cw := &captureStatus{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(cw, r)
		h.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", cw.code,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

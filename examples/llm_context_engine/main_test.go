package main

import (
	"net/http"
	"testing"
	"time"
)

type blockingTransport struct{}

func (blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestOllamaHTTPRequestsAreBounded(t *testing.T) {
	if ollamaHTTPClient.Timeout != ollamaRequestTimeout || ollamaRequestTimeout <= 0 {
		t.Fatalf("production client timeout: got %s want %s", ollamaHTTPClient.Timeout, ollamaRequestTimeout)
	}

	client := &http.Client{Transport: blockingTransport{}, Timeout: 20 * time.Millisecond}
	start := time.Now()
	if _, err := embedWithClient(client, "http://ollama.invalid", "test"); err == nil {
		t.Fatal("embedWithClient returned nil error after timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("embedWithClient timeout took %s", elapsed)
	}

	start = time.Now()
	if ollamaReachableWithClient(client, "http://ollama.invalid") {
		t.Fatal("ollamaReachableWithClient reported a timed-out request as reachable")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ollamaReachableWithClient timeout took %s", elapsed)
	}
}

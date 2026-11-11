package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hexxla/hexxladb"
)

type blockingTransport struct{ started chan struct{} }

func (transport blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if transport.started != nil {
		close(transport.started)
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestReplacingSearchCancelsOllamaAndKeepsNewResults(t *testing.T) {
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "search.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key, err := hexxladb.Pack(hexxladb.Coord{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.NewFactCell(key, "replacement match", "test", "test", 1))
	}); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		originalClient := ollamaHTTPClient
		ollamaHTTPClient = &http.Client{Transport: blockingTransport{started: started}}
		defer func() { ollamaHTTPClient = originalClient }()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		view := newCellsView(ctx, db)
		view.searchMode, view.input = "embedding", "superseded"
		previous := view.executeSearch()
		oldReply := make(chan tea.Msg, 1)
		go func() { oldReply <- previous() }()
		<-started

		view.searchMode, view.input = "lexical", "replacement"
		view.Update(view.executeSearch()())
		synctest.Wait()
		select {
		case reply := <-oldReply:
			if msg := reply.(cellsLoadedMsg); !errors.Is(msg.err, context.Canceled) {
				t.Fatalf("superseded search error = %v, want context.Canceled", msg.err)
			}
			view.Update(reply)
		default:
			t.Fatal("superseded Ollama request was not canceled")
		}
		if view.err != nil || len(view.searchHits) != 1 || view.searchHits[0].cell.RawContent != "replacement match" {
			t.Fatalf("replacement search results = %+v, error = %v", view.searchHits, view.err)
		}
	})
}

func TestOllamaHTTPRequestsAreBounded(t *testing.T) {
	if ollamaHTTPClient.Timeout != ollamaRequestTimeout || ollamaRequestTimeout <= 0 {
		t.Fatalf("production client timeout: got %s want %s", ollamaHTTPClient.Timeout, ollamaRequestTimeout)
	}

	client := &http.Client{Transport: blockingTransport{}, Timeout: 20 * time.Millisecond}
	start := time.Now()
	if _, err := embedWithClient(t.Context(), client, "http://ollama.invalid", "test"); err == nil {
		t.Fatal("embedWithClient returned nil error after timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("embedWithClient timeout took %s", elapsed)
	}

}

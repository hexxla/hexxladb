package main

import (
	"errors"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestFitRenderedPromptCountsCompleteRequest(t *testing.T) {
	candidates := []hexxladb.CellView{
		{RawContent: "first memory"},
		{RawContent: "second memory"},
	}
	counter := func(rendered string) (int, error) {
		return len(rendered), nil
	}
	base := renderModelRequest([]string{"be concise"}, nil, "question")
	withFirst := renderModelRequest([]string{"be concise"}, candidates[:1], "question")

	selected, prompt, used, err := fitRenderedPrompt(
		[]string{"be concise"},
		candidates,
		"question",
		len(withFirst),
		counter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].RawContent != "first memory" {
		t.Fatalf("selected = %+v, want first candidate only", selected)
	}
	if prompt != withFirst || used != len(withFirst) {
		t.Fatalf("prompt usage = %d, want %d; prompt matches = %t", used, len(withFirst), prompt == withFirst)
	}
	if used <= len("first memory") || used <= len(base) {
		t.Fatal("counter did not include both fixed request overhead and selected memory")
	}
}

func TestFitRenderedPromptRejectsRequiredOverhead(t *testing.T) {
	_, _, _, err := fitRenderedPrompt(nil, nil, "required user message", 1, func(rendered string) (int, error) {
		return len(rendered), nil
	})
	if !errors.Is(err, errPromptBudgetTooSmall) {
		t.Fatalf("error = %v, want %v", err, errPromptBudgetTooSmall)
	}
}

func TestFitRenderedPromptPropagatesCounterFailure(t *testing.T) {
	want := errors.New("counter failed")
	_, _, _, err := fitRenderedPrompt(nil, nil, "question", 100, func(string) (int, error) {
		return 0, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped %v", err, want)
	}
}

func TestRankByConfidenceDoesNotMutateRetrievalOrder(t *testing.T) {
	cells := []hexxladb.CellView{
		{RawContent: "low", Provenance: hexxladb.ProvenanceWire{Confidence: 0.2}},
		{RawContent: "high", Provenance: hexxladb.ProvenanceWire{Confidence: 0.9}},
	}
	ranked := rankByConfidence(cells)
	if ranked[0].RawContent != "high" {
		t.Fatalf("ranked first = %q, want high", ranked[0].RawContent)
	}
	if cells[0].RawContent != "low" {
		t.Fatal("caller ranking mutated the database retrieval order")
	}
}

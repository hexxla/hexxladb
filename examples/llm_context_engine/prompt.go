package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hexxla/hexxladb"
)

var errPromptBudgetTooSmall = errors.New("prompt budget is too small for required request content")

type renderedTokenCounter func(string) (int, error)

// rankByConfidence demonstrates an application-owned policy. HexxlaDB returns
// deterministic candidates and does not silently reinterpret confidence.
func rankByConfidence(cells []hexxladb.CellView) []hexxladb.CellView {
	ranked := slices.Clone(cells)
	slices.SortStableFunc(ranked, func(a, b hexxladb.CellView) int {
		return cmp.Compare(b.Provenance.Confidence, a.Provenance.Confidence)
	})
	return ranked
}

// fitRenderedPrompt checks the complete rendered request after each candidate
// is added. The application supplies the provider/model-specific counter, so
// HexxlaDB never needs tokenizer dependencies or provider metadata.
func fitRenderedPrompt(preferences []string, candidates []hexxladb.CellView, userMessage string, maxTokens int, count renderedTokenCounter) ([]hexxladb.CellView, string, int, error) {
	if maxTokens <= 0 || count == nil {
		return nil, "", 0, errPromptBudgetTooSmall
	}
	selected := make([]hexxladb.CellView, 0, len(candidates))
	prompt := renderModelRequest(preferences, selected, userMessage)
	used, err := count(prompt)
	if err != nil {
		return nil, "", 0, fmt.Errorf("count required prompt tokens: %w", err)
	}
	if used > maxTokens {
		return nil, "", used, errPromptBudgetTooSmall
	}
	for _, candidate := range candidates {
		proposed := append(selected, candidate)
		rendered := renderModelRequest(preferences, proposed, userMessage)
		tokens, err := count(rendered)
		if err != nil {
			return nil, "", 0, fmt.Errorf("count rendered prompt tokens: %w", err)
		}
		if tokens > maxTokens {
			continue
		}
		selected = proposed
		prompt = rendered
		used = tokens
	}
	return selected, prompt, used, nil
}

func renderModelRequest(preferences []string, contextCells []hexxladb.CellView, userMessage string) string {
	var prompt strings.Builder
	prompt.WriteString("SYSTEM\nYou are a helpful coding assistant.\n")
	prompt.WriteString("USER PREFERENCES\n")
	for _, preference := range preferences {
		fmt.Fprintf(&prompt, "- %s\n", preference)
	}
	prompt.WriteString("RELEVANT MEMORY\n")
	for _, cell := range contextCells {
		fmt.Fprintf(&prompt, "- %s\n", cell.RawContent)
	}
	prompt.WriteString("USER\n")
	prompt.WriteString(userMessage)
	return prompt.String()
}

// demoWordCounter keeps the example runnable without choosing a provider. It
// deliberately is not called a tokenizer and is replaced at this application
// seam when integrating a real model.
func demoWordCounter(rendered string) (int, error) {
	return len(strings.Fields(rendered)), nil
}

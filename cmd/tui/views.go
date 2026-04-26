package main

import tea "github.com/charmbracelet/bubbletea"

// view is the interface every tab panel implements.
type view interface {
	Update(msg tea.Msg) (view, tea.Cmd)
	View() string
	SetSize(w, h int)
	// Consuming reports whether the tab is in a text-input mode (e.g. search
	// bar open). When true, the model skips global shortcuts so keypresses
	// reach the tab's Update method instead.
	Consuming() bool
}

// noConsume is embedded in views that never consume keypresses globally.
type noConsume struct{}

func (noConsume) Consuming() bool { return false }

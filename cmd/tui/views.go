package main

import tea "github.com/charmbracelet/bubbletea"

// view is the interface every tab panel implements.
type view interface {
	Update(msg tea.Msg) (view, tea.Cmd)
	View() string
	SetSize(w, h int)
}

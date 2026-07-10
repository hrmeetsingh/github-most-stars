// Command github-most-stars is a terminal UI that continuously scans public
// GitHub repositories for the most-starred and recently-trending projects.
package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hrmeetsingh/github-most-stars/internal/scanner"
	"github.com/hrmeetsingh/github-most-stars/internal/ui"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := scanner.New()
	go s.Run(ctx)

	m := ui.New(s, cancel)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error running program:", err)
		os.Exit(1)
	}
}

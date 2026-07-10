// Package scanner runs the background polling loops that repeatedly query
// GitHub for the top-starred and trending repositories, emitting results
// on a channel for the UI to consume.
package scanner

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hrmeetsingh/github-most-stars/internal/github"
)

// Category identifies which of the three panels a Result belongs to.
type Category int

const (
	TopStars Category = iota
	WeeklyTrending
	MonthlyTrending
)

// pollInterval is how often each individual query category is re-run.
// Three categories staggered across this interval keeps us well under
// GitHub's unauthenticated search rate limit of 10 requests/min.
const pollInterval = 45 * time.Second

// Result is emitted whenever a poll for a category completes, successfully
// or not.
type Result struct {
	Category Category
	Repos    []github.Repo
	Err      error
	At       time.Time
}

// StatusMsg is emitted whenever the scanner's running/paused state changes.
type StatusMsg struct {
	Paused bool
}

// Scanner owns the background polling goroutines.
type Scanner struct {
	client  *github.Client
	results chan Result
	status  chan StatusMsg
	paused  atomic.Bool
}

// New creates a Scanner. Call Run to start polling in the background.
func New() *Scanner {
	return &Scanner{
		client:  github.NewClient(),
		results: make(chan Result, 8),
		status:  make(chan StatusMsg, 4),
	}
}

// Results returns the channel on which poll results are delivered.
func (s *Scanner) Results() <-chan Result { return s.results }

// Status returns the channel on which pause/resume state changes are delivered.
func (s *Scanner) Status() <-chan StatusMsg { return s.status }

// TogglePause flips the paused state and reports the new state.
func (s *Scanner) TogglePause() bool {
	newState := !s.paused.Load()
	s.paused.Store(newState)
	s.status <- StatusMsg{Paused: newState}
	return newState
}

// Paused reports whether polling is currently paused.
func (s *Scanner) Paused() bool { return s.paused.Load() }

type query struct {
	category Category
	build    func() string
	sort     string
	order    string
	start    time.Duration // initial stagger delay
}

// Run starts the three staggered polling loops. It blocks until ctx is
// cancelled, at which point all goroutines exit and results/status
// channels are closed.
func (s *Scanner) Run(ctx context.Context) {
	queries := []query{
		{
			category: TopStars,
			build:    func() string { return "stars:>1" },
			sort:     "stars",
			order:    "desc",
			start:    0,
		},
		{
			category: WeeklyTrending,
			build: func() string {
				return fmt.Sprintf("created:>%s", time.Now().AddDate(0, 0, -7).Format("2006-01-02"))
			},
			sort:  "stars",
			order: "desc",
			start: 15 * time.Second,
		},
		{
			category: MonthlyTrending,
			build: func() string {
				return fmt.Sprintf("created:>%s", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
			},
			sort:  "stars",
			order: "desc",
			start: 30 * time.Second,
		},
	}

	done := make(chan struct{}, len(queries))
	for _, q := range queries {
		go s.pollLoop(ctx, q, done)
	}

	for range queries {
		<-done
	}
	close(s.results)
	close(s.status)
}

func (s *Scanner) pollLoop(ctx context.Context, q query, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(q.start)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !s.paused.Load() {
				s.poll(ctx, q)
			}
			timer.Reset(pollInterval)
		}
	}
}

func (s *Scanner) poll(ctx context.Context, q query) {
	repos, err := s.client.SearchRepos(ctx, q.build(), q.sort, q.order, 10)
	if ctx.Err() != nil {
		return
	}
	select {
	case s.results <- Result{Category: q.category, Repos: repos, Err: err, At: time.Now()}:
	case <-ctx.Done():
	}
}

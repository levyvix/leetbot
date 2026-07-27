// Package bot implements the harvest -> test -> submit loop.
package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/levyvix/leetbot/internal/leetcode"
)

// Status is the outcome of attempting one problem.
type Status string

const (
	StatusAccepted Status = "accepted" // submitted and accepted
	StatusTested   Status = "tested"   // passed the examples, not submitted (dry run)
	StatusFailed   Status = "failed"   // no candidate solution worked
	StatusSkipped  Status = "skipped"  // premium, unsupported type, already solved
	StatusError    Status = "error"    // transport or API failure
)

// Outcome records what happened with one problem.
type Outcome struct {
	Slug       string    `json:"slug"`
	FrontendID int       `json:"frontend_id"`
	Status     Status    `json:"status"`
	Detail     string    `json:"detail"`
	Code       string    `json:"code,omitempty"`
	Attempts   int       `json:"attempts"`
	At         time.Time `json:"at"`
}

// Config tunes the solver.
type Config struct {
	Lang          string        // LeetCode language slug, e.g. "python3"
	Submit        bool          // when false, stop after the example tests pass
	MaxArticles   int           // how many top-voted solution posts to read
	MaxCandidates int           // how many code blocks to actually test
	PauseBetween  time.Duration // delay between problems
}

// DefaultConfig is a deliberately conservative setup.
func DefaultConfig() Config {
	return Config{
		Lang:          "python3",
		Submit:        false,
		MaxArticles:   8,
		MaxCandidates: 5,
		PauseBetween:  5 * time.Second,
	}
}

// Bot solves problems using top-voted community solutions.
type Bot struct {
	client *leetcode.Client
	cfg    Config
	Log    func(format string, args ...any)
}

// New builds a Bot. log may be nil.
func New(client *leetcode.Client, cfg Config, log func(string, ...any)) *Bot {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Bot{client: client, cfg: cfg, Log: log}
}

// unsupportedTags are problem categories whose submission flow differs enough
// that the example-test step does not apply.
var unsupportedTags = map[string]bool{
	"database":    true,
	"shell":       true,
	"concurrency": true,
}

// Solve attempts a single problem.
func (b *Bot) Solve(ctx context.Context, entry leetcode.IndexEntry) Outcome {
	out := Outcome{Slug: entry.Slug, FrontendID: entry.FrontendID, At: time.Now()}

	if entry.PaidOnly {
		out.Status, out.Detail = StatusSkipped, "premium-only"
		return out
	}

	q, err := b.client.GetQuestion(ctx, entry.Slug)
	if err != nil {
		out.Status, out.Detail = StatusError, err.Error()
		return out
	}
	for _, t := range q.TopicTags {
		if unsupportedTags[t.Slug] {
			out.Status, out.Detail = StatusSkipped, "categoria não suportada: "+t.Slug
			return out
		}
	}
	entryPoint := q.EntryPoint()
	if entryPoint == "" {
		out.Status, out.Detail = StatusSkipped, "sem entry point no metaData"
		return out
	}
	if _, ok := q.Snippet(b.cfg.Lang); !ok {
		out.Status, out.Detail = StatusSkipped, "sem stub para "+b.cfg.Lang
		return out
	}
	if strings.TrimSpace(q.ExampleTestcases) == "" {
		out.Status, out.Detail = StatusSkipped, "sem casos de exemplo"
		return out
	}

	articles, err := b.client.ListSolutions(ctx, entry.Slug, b.cfg.Lang, b.cfg.MaxArticles)
	if err != nil {
		out.Status, out.Detail = StatusError, err.Error()
		return out
	}
	if len(articles) == 0 {
		out.Status, out.Detail = StatusFailed, "nenhuma solução da comunidade em "+b.cfg.Lang
		return out
	}

	tried := 0
	var lastDetail string

	for _, art := range articles {
		if tried >= b.cfg.MaxCandidates {
			break
		}
		md, err := b.client.GetSolutionContent(ctx, art.TopicID)
		if err != nil {
			lastDetail = err.Error()
			continue
		}
		for _, code := range leetcode.ExtractCode(md, b.cfg.Lang, entryPoint) {
			if tried >= b.cfg.MaxCandidates {
				break
			}
			tried++
			out.Attempts = tried
			b.Log("  [%d/%d] testando candidato de %q", tried, b.cfg.MaxCandidates, truncate(art.Title, 48))

			run, err := b.client.RunCode(ctx, q, b.cfg.Lang, code, q.ExampleTestcases)
			if err != nil {
				lastDetail = err.Error()
				continue
			}
			if !run.RunSuccess || !run.CorrectAnswer {
				lastDetail = "exemplos: " + run.Error()
				b.Log("      reprovado nos exemplos: %s", truncate(run.Error(), 90))
				continue
			}

			if !b.cfg.Submit {
				out.Status, out.Detail, out.Code = StatusTested, "passou nos exemplos (dry-run)", code
				return out
			}

			sub, err := b.client.SubmitCode(ctx, q, b.cfg.Lang, code)
			if err != nil {
				lastDetail = err.Error()
				continue
			}
			if sub.Accepted() {
				out.Status = StatusAccepted
				out.Detail = fmt.Sprintf("%s | %s | %s", sub.StatusMsg, sub.StatusRuntime, sub.StatusMemory)
				out.Code = code
				return out
			}
			lastDetail = fmt.Sprintf("submissão: %s (%d/%d)", sub.StatusMsg, sub.TotalCorrect, sub.TotalTestcases)
			b.Log("      submissão rejeitada: %s", truncate(lastDetail, 90))
		}
	}

	out.Status = StatusFailed
	if lastDetail == "" {
		lastDetail = "nenhum bloco de código utilizável encontrado"
	}
	out.Detail = lastDetail
	return out
}

// Run solves every entry in order, persisting progress after each one.
func (b *Bot) Run(ctx context.Context, entries []leetcode.IndexEntry, state *State) error {
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if prev, ok := state.Get(e.Slug); ok && prev.Status != StatusError {
			b.Log("[%d/%d] %d. %s — já processado (%s)", i+1, len(entries), e.FrontendID, e.Slug, prev.Status)
			continue
		}

		b.Log("[%d/%d] %d. %s", i+1, len(entries), e.FrontendID, e.Slug)
		out := b.Solve(ctx, e)
		b.Log("  -> %s: %s", out.Status, truncate(out.Detail, 110))

		state.Put(out)
		if err := state.Save(); err != nil {
			return fmt.Errorf("salvando estado: %w", err)
		}

		if i < len(entries)-1 && b.cfg.PauseBetween > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(b.cfg.PauseBetween):
			}
		}
	}
	return nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

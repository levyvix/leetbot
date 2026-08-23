package bot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/levyvix/leetbot/internal/leetcode"
)

// fakeAPI is a scriptable stand-in for *leetcode.Client.
type fakeAPI struct {
	question *leetcode.Question
	articles []leetcode.SolutionArticle
	content  string

	questionErr error
	listErr     error
	contentErr  error
	runErr      error
	submitErr   error

	run    *leetcode.CheckResult
	submit *leetcode.CheckResult

	// submitFn, when set, overrides submit/submitErr per call.
	submitFn func() (*leetcode.CheckResult, error)

	runs    int
	submits int
}

func (f *fakeAPI) GetQuestion(context.Context, string) (*leetcode.Question, error) {
	return f.question, f.questionErr
}

func (f *fakeAPI) ListSolutions(context.Context, string, string, int) ([]leetcode.SolutionArticle, error) {
	return f.articles, f.listErr
}

func (f *fakeAPI) GetSolutionContent(context.Context, int64) (string, error) {
	return f.content, f.contentErr
}

func (f *fakeAPI) RunCode(context.Context, *leetcode.Question, string, string, string) (*leetcode.CheckResult, error) {
	f.runs++
	return f.run, f.runErr
}

func (f *fakeAPI) SubmitCode(context.Context, *leetcode.Question, string, string) (*leetcode.CheckResult, error) {
	f.submits++
	if f.submitFn != nil {
		return f.submitFn()
	}
	return f.submit, f.submitErr
}

// solvable builds a fake that hands out one working python3 solution.
func solvable() *fakeAPI {
	return &fakeAPI{
		question: &leetcode.Question{
			TitleSlug:        "two-sum",
			QuestionID:       "1",
			MetaData:         `{"name":"twoSum"}`,
			ExampleTestcases: "[2,7,11,15]\n9",
			CodeSnippets:     []leetcode.CodeSnippet{{LangSlug: "python3", Code: "class Solution:"}},
		},
		articles: []leetcode.SolutionArticle{{Title: "O(n) hash map", TopicID: 7}},
		content:  "explicação\n\n```python3\nclass Solution:\n    def twoSum(self): pass\n```\n",
		run:      &leetcode.CheckResult{RunSuccess: true, CorrectAnswer: true},
		submit:   &leetcode.CheckResult{StatusCode: leetcode.StatusAccepted, StatusMsg: "Accepted"},
	}
}

func testCfg() Config {
	cfg := DefaultConfig()
	cfg.PauseBetween = 0
	return cfg
}

func TestSolveSkips(t *testing.T) {
	for _, tc := range []struct {
		name       string
		entry      leetcode.IndexEntry
		mutate     func(*fakeAPI)
		wantDetail string
	}{
		{
			name:       "premium",
			entry:      leetcode.IndexEntry{Slug: "two-sum", PaidOnly: true},
			wantDetail: "premium-only",
		},
		{
			name:       "categoria não suportada",
			mutate:     func(f *fakeAPI) { f.question.TopicTags = []leetcode.TopicTag{{Slug: "database"}} },
			wantDetail: "categoria não suportada: database",
		},
		{
			name:       "sem entry point",
			mutate:     func(f *fakeAPI) { f.question.MetaData = `{"mysql":[]}` },
			wantDetail: "sem entry point no metaData",
		},
		{
			name:       "sem stub na linguagem",
			mutate:     func(f *fakeAPI) { f.question.CodeSnippets = nil },
			wantDetail: "sem stub para python3",
		},
		{
			name:       "sem casos de exemplo",
			mutate:     func(f *fakeAPI) { f.question.ExampleTestcases = "  " },
			wantDetail: "sem casos de exemplo",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := solvable()
			if tc.mutate != nil {
				tc.mutate(api)
			}
			entry := tc.entry
			if entry.Slug == "" {
				entry = leetcode.IndexEntry{Slug: "two-sum"}
			}

			out, err := New(api, testCfg(), nil).Solve(t.Context(), entry)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if out.Status != StatusSkipped {
				t.Errorf("Status = %s, want %s", out.Status, StatusSkipped)
			}
			if out.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", out.Detail, tc.wantDetail)
			}
			if api.runs != 0 || api.submits != 0 {
				t.Errorf("problema pulado não deveria bater na rede (runs=%d submits=%d)", api.runs, api.submits)
			}
		})
	}
}

func TestSolveDryRunDoesNotSubmit(t *testing.T) {
	api := solvable()
	cfg := testCfg()
	cfg.Submit = false

	out, err := New(api, cfg, nil).Solve(t.Context(), leetcode.IndexEntry{Slug: "two-sum"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusTested {
		t.Errorf("Status = %s (%s), want %s", out.Status, out.Detail, StatusTested)
	}
	if api.submits != 0 {
		t.Errorf("dry-run submeteu %d vez(es)", api.submits)
	}
	if !strings.Contains(out.Code, "twoSum") {
		t.Errorf("código aprovado não foi guardado: %q", out.Code)
	}
}

func TestSolveSubmitAccepted(t *testing.T) {
	api := solvable()
	cfg := testCfg()
	cfg.Submit = true

	out, err := New(api, cfg, nil).Solve(t.Context(), leetcode.IndexEntry{Slug: "two-sum"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusAccepted {
		t.Errorf("Status = %s (%s), want %s", out.Status, out.Detail, StatusAccepted)
	}
	if api.submits != 1 {
		t.Errorf("submits = %d, want 1", api.submits)
	}
}

func TestSolveStopsAtMaxCandidates(t *testing.T) {
	api := solvable()
	api.content = strings.Repeat("```python3\nclass Solution:\n    def twoSum(self): pass\n```\n\n", 10)
	api.run = &leetcode.CheckResult{RunSuccess: false, StatusMsg: "Wrong Answer"}
	cfg := testCfg()
	cfg.MaxCandidates = 3

	out, err := New(api, cfg, nil).Solve(t.Context(), leetcode.IndexEntry{Slug: "two-sum"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusFailed {
		t.Errorf("Status = %s, want %s", out.Status, StatusFailed)
	}
	if api.runs != 3 {
		t.Errorf("runs = %d, want 3 (MaxCandidates)", api.runs)
	}
	if out.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", out.Attempts)
	}
}

func TestSolveUnauthorizedIsFatal(t *testing.T) {
	api := solvable()
	api.questionErr = fmt.Errorf("GET /x: 403: %w", leetcode.ErrUnauthorized)

	out, err := New(api, testCfg(), nil).Solve(t.Context(), leetcode.IndexEntry{Slug: "two-sum"})
	if !errors.Is(err, leetcode.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized propagado", err)
	}
	if out.Status != StatusError {
		t.Errorf("Status = %s, want %s", out.Status, StatusError)
	}
}

func TestSolveTransportErrorIsNotFatal(t *testing.T) {
	api := solvable()
	api.questionErr = errors.New("connection reset")

	out, err := New(api, testCfg(), nil).Solve(t.Context(), leetcode.IndexEntry{Slug: "two-sum"})
	if err != nil {
		t.Errorf("erro transitório não deveria abortar o run: %v", err)
	}
	if out.Status != StatusError {
		t.Errorf("Status = %s, want %s", out.Status, StatusError)
	}
}

func TestRunAbortsOnUnauthorized(t *testing.T) {
	api := solvable()
	api.questionErr = fmt.Errorf("GET /x: 401: %w", leetcode.ErrUnauthorized)
	state := newTestState(t)

	entries := []leetcode.IndexEntry{{Slug: "a"}, {Slug: "b"}, {Slug: "c"}}
	err := New(api, testCfg(), nil).Run(t.Context(), entries, state)
	if !errors.Is(err, leetcode.ErrUnauthorized) {
		t.Fatalf("Run err = %v, want ErrUnauthorized", err)
	}
	// Só o primeiro problema foi tentado, e o resultado ficou salvo.
	if _, ok := state.Get("a"); !ok {
		t.Error("o outcome do primeiro problema não foi persistido antes de abortar")
	}
	if _, ok := state.Get("b"); ok {
		t.Error("Run continuou depois de um erro fatal")
	}
}

func TestRunSkipsAlreadyProcessed(t *testing.T) {
	api := solvable()
	state := newTestState(t)
	state.Put(Outcome{Slug: "a", Status: StatusTested})
	state.Put(Outcome{Slug: "b", Status: StatusError}) // erros são retentados

	entries := []leetcode.IndexEntry{{Slug: "a"}, {Slug: "b"}}
	if err := New(api, testCfg(), nil).Run(t.Context(), entries, state); err != nil {
		t.Fatal(err)
	}
	if api.runs != 1 {
		t.Errorf("runs = %d, want 1 (só o outcome com erro é retentado)", api.runs)
	}
	if o, _ := state.Get("a"); o.Status != StatusTested {
		t.Errorf("outcome já processado foi sobrescrito: %+v", o)
	}
}

func TestRunRetriesFailedWhenConfigured(t *testing.T) {
	api := solvable()
	state := newTestState(t)
	state.Put(Outcome{Slug: "failed", Status: StatusFailed})
	state.Put(Outcome{Slug: "tested", Status: StatusTested})

	cfg := testCfg()
	cfg.RetryFailed = true
	if err := New(api, cfg, nil).Run(t.Context(), []leetcode.IndexEntry{
		{Slug: "failed"}, {Slug: "tested"},
	}, state); err != nil {
		t.Fatal(err)
	}
	if api.runs != 1 {
		t.Errorf("runs = %d, want 1 (only failed outcome is retried)", api.runs)
	}
	if out, _ := state.Get("failed"); out.Status != StatusTested {
		t.Errorf("failed outcome was not retried: %+v", out)
	}
}

func TestRunStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	api := solvable()
	err := New(api, testCfg(), nil).Run(ctx, []leetcode.IndexEntry{{Slug: "a"}}, newTestState(t))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run err = %v, want context.Canceled", err)
	}
	if api.runs != 0 {
		t.Errorf("Run executou %d candidato(s) com contexto cancelado", api.runs)
	}
}

func TestSolveAbandonsProblemWhenRateLimited(t *testing.T) {
	api := solvable()
	api.content = "```python3\nclass Solution:\n    def twoSum(self): pass\n```\n" +
		"```python3\nclass Solution:\n    def twoSum(self): return 1\n```\n"
	api.submit, api.submitErr = nil, fmt.Errorf("submit: %w", leetcode.ErrRateLimited)

	cfg := testCfg()
	cfg.Submit = true
	out, err := New(api, cfg, nil).Solve(t.Context(), leetcode.IndexEntry{Slug: "two-sum"})
	if !errors.Is(err, leetcode.ErrRateLimited) {
		t.Fatalf("Solve err = %v, want ErrRateLimited (Run conta os consecutivos)", err)
	}
	if out.Status != StatusError {
		t.Errorf("status = %s, want %s", out.Status, StatusError)
	}
	if api.submits != 1 {
		t.Errorf("%d submissões, want 1 — o próximo candidato tomaria 429 igual", api.submits)
	}
}

func TestRunStopsAfterConsecutiveRateLimits(t *testing.T) {
	api := solvable()
	api.submit, api.submitErr = nil, fmt.Errorf("submit: %w", leetcode.ErrRateLimited)

	cfg := testCfg()
	cfg.Submit = true
	queue := make([]leetcode.IndexEntry, 20)
	for i := range queue {
		queue[i] = leetcode.IndexEntry{Slug: fmt.Sprintf("p%d", i), FrontendID: i}
	}

	err := New(api, cfg, nil).Run(t.Context(), queue, newTestState(t))
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("Run err = %v, want ErrQuotaExhausted", err)
	}
	if api.submits != maxRateLimitedInARow {
		t.Errorf("%d submissões, want %d — a fila inteira não devia virar erro", api.submits, maxRateLimitedInARow)
	}
}

func TestRunRateLimitCounterResetsOnAccepted(t *testing.T) {
	api := solvable()
	accepted := api.submit
	// Alterna: bloqueia, passa, bloqueia, passa… nunca chega a 3 seguidos.
	var n int
	api.submitFn = func() (*leetcode.CheckResult, error) {
		if n++; n%2 == 1 {
			return nil, fmt.Errorf("submit: %w", leetcode.ErrRateLimited)
		}
		return accepted, nil
	}

	cfg := testCfg()
	cfg.Submit = true
	queue := make([]leetcode.IndexEntry, 8)
	for i := range queue {
		queue[i] = leetcode.IndexEntry{Slug: fmt.Sprintf("p%d", i), FrontendID: i}
	}

	if err := New(api, cfg, nil).Run(t.Context(), queue, newTestState(t)); err != nil {
		t.Fatalf("Run err = %v, want nil — 429 intercalado com aceite não é cota esgotada", err)
	}
}

func newTestState(t *testing.T) *State {
	t.Helper()
	s, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

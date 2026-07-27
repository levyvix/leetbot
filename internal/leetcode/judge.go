package leetcode

import (
	"context"
	"fmt"
	"time"
)

// Judge status codes returned by /submissions/detail/{id}/check/.
const (
	StatusAccepted            = 10
	StatusWrongAnswer         = 11
	StatusMemoryLimitExceeded = 12
	StatusOutputLimitExceeded = 13
	StatusTimeLimitExceeded   = 14
	StatusRuntimeError        = 15
	StatusCompileError        = 20
)

// CheckResult is the judge's verdict. The endpoint returns a superset of
// fields for run vs submit; the ones both share are declared here.
type CheckResult struct {
	State          string `json:"state"` // PENDING, STARTED, SUCCESS
	StatusCode     int    `json:"status_code"`
	StatusMsg      string `json:"status_msg"`
	RunSuccess     bool   `json:"run_success"`
	TotalCorrect   int    `json:"total_correct"`
	TotalTestcases int    `json:"total_testcases"`
	StatusRuntime  string `json:"status_runtime"`
	StatusMemory   string `json:"status_memory"`

	// Run-only.
	CorrectAnswer bool     `json:"correct_answer"`
	CodeAnswer    []string `json:"code_answer"`

	// Failure detail.
	LastTestcase     string `json:"last_testcase"`
	ExpectedOutput   string `json:"expected_output"`
	CompileError     string `json:"compile_error"`
	FullCompileError string `json:"full_compile_error"`
	FullRuntimeError string `json:"full_runtime_error"`
}

// Accepted reports whether a submission passed every test case.
func (r *CheckResult) Accepted() bool {
	return r.StatusCode == StatusAccepted
}

// Error returns the most useful failure message available, or "".
func (r *CheckResult) Error() string {
	switch {
	case r.FullCompileError != "":
		return r.FullCompileError
	case r.CompileError != "":
		return r.CompileError
	case r.FullRuntimeError != "":
		return r.FullRuntimeError
	case r.LastTestcase != "":
		return fmt.Sprintf("falhou em %q (esperado %q)", truncate(r.LastTestcase, 120), truncate(r.ExpectedOutput, 120))
	default:
		return r.StatusMsg
	}
}

// RunCode runs code against dataInput without submitting ("Run Code").
func (c *Client) RunCode(ctx context.Context, q *Question, langSlug, code, dataInput string) (*CheckResult, error) {
	var resp struct {
		InterpretID string `json:"interpret_id"`
	}
	body := map[string]any{
		"lang":        langSlug,
		"question_id": q.QuestionID,
		"typed_code":  code,
		"data_input":  dataInput,
	}
	path := "/problems/" + q.TitleSlug + "/interpret_solution/"
	if err := c.postJSON(ctx, path, problemRef(q.TitleSlug), body, &resp); err != nil {
		return nil, err
	}
	if resp.InterpretID == "" {
		return nil, fmt.Errorf("interpret_solution não retornou interpret_id")
	}
	return c.PollResult(ctx, resp.InterpretID)
}

// SubmitCode submits a solution and returns the judge's verdict.
func (c *Client) SubmitCode(ctx context.Context, q *Question, langSlug, code string) (*CheckResult, error) {
	var resp struct {
		SubmissionID int64 `json:"submission_id"`
	}
	body := map[string]any{
		"lang":         langSlug,
		"question_id":  q.QuestionID,
		"questionSlug": q.TitleSlug,
		"typed_code":   code,
	}
	path := "/problems/" + q.TitleSlug + "/submit/"
	if err := c.postJSON(ctx, path, problemRef(q.TitleSlug), body, &resp); err != nil {
		return nil, err
	}
	if resp.SubmissionID == 0 {
		return nil, fmt.Errorf("submit não retornou submission_id")
	}
	return c.PollResult(ctx, fmt.Sprint(resp.SubmissionID))
}

// PollResult polls the judge until the run leaves the PENDING/STARTED states.
func (c *Client) PollResult(ctx context.Context, id string) (*CheckResult, error) {
	const (
		maxWait = 90 * time.Second
		tick    = 700 * time.Millisecond
	)
	deadline := time.Now().Add(maxWait)
	path := "/submissions/detail/" + id + "/check/"

	for time.Now().Before(deadline) {
		var r CheckResult
		if err := c.getJSON(ctx, path, BaseURL+"/", &r); err != nil {
			return nil, err
		}
		if r.State != "PENDING" && r.State != "STARTED" {
			return &r, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(tick):
		}
	}
	return nil, fmt.Errorf("timeout esperando o julgamento de %s", id)
}

func problemRef(slug string) string {
	return BaseURL + "/problems/" + slug + "/"
}

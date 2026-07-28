package leetcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Difficulty levels as reported by /api/problems/all/.
const (
	DiffEasy   = 1
	DiffMedium = 2
	DiffHard   = 3
)

// IndexEntry is one problem in the global index.
type IndexEntry struct {
	QuestionID int    `json:"question_id"` // internal id, required by /submit/
	FrontendID int    `json:"frontend_id"` // the number shown in the UI
	Title      string `json:"title"`
	Slug       string `json:"slug"`
	Difficulty int    `json:"difficulty"`
	PaidOnly   bool   `json:"paid_only"`
	Solved     bool   `json:"solved"` // status == "ac" for the logged in user
}

// Index maps problem ids and slugs to entries.
type Index struct {
	FetchedAt time.Time    `json:"fetched_at"`
	Entries   []IndexEntry `json:"entries"`
}

// IsID reports whether ref is a bare frontend id rather than a slug. Slugs may
// start with digits ("3sum", "2-keys-keyboard"), so the whole string must parse.
func IsID(ref string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(ref))
	return n, err == nil
}

// Lookup finds a problem by frontend id (e.g. "1") or slug (e.g. "two-sum").
func (idx *Index) Lookup(ref string) (IndexEntry, bool) {
	if n, ok := IsID(ref); ok {
		for _, e := range idx.Entries {
			if e.FrontendID == n {
				return e, true
			}
		}
		return IndexEntry{}, false
	}
	slug := strings.ToLower(strings.TrimSpace(ref))
	for _, e := range idx.Entries {
		if e.Slug == slug {
			return e, true
		}
	}
	return IndexEntry{}, false
}

// FetchIndex downloads the full problem list. Authenticated, so "solved"
// reflects the current user.
func (c *Client) FetchIndex(ctx context.Context) (*Index, error) {
	var resp struct {
		StatStatusPairs []struct {
			Stat struct {
				QuestionID int    `json:"question_id"`
				FrontendID int    `json:"frontend_question_id"`
				Title      string `json:"question__title"`
				Slug       string `json:"question__title_slug"`
			} `json:"stat"`
			Difficulty struct {
				Level int `json:"level"`
			} `json:"difficulty"`
			PaidOnly bool   `json:"paid_only"`
			Status   string `json:"status"`
		} `json:"stat_status_pairs"`
	}
	if err := c.getJSON(ctx, "/api/problems/all/", BaseURL+"/problemset/all/", &resp); err != nil {
		return nil, err
	}
	if len(resp.StatStatusPairs) == 0 {
		return nil, fmt.Errorf("índice vazio — a API mudou ou a sessão caiu")
	}

	idx := &Index{FetchedAt: time.Now(), Entries: make([]IndexEntry, 0, len(resp.StatStatusPairs))}
	for _, p := range resp.StatStatusPairs {
		idx.Entries = append(idx.Entries, IndexEntry{
			QuestionID: p.Stat.QuestionID,
			FrontendID: p.Stat.FrontendID,
			Title:      p.Stat.Title,
			Slug:       p.Stat.Slug,
			Difficulty: p.Difficulty.Level,
			PaidOnly:   p.PaidOnly,
			Solved:     p.Status == "ac",
		})
	}
	return idx, nil
}

// LoadIndex returns a cached index, refetching when older than maxAge.
func (c *Client) LoadIndex(ctx context.Context, path string, maxAge time.Duration) (*Index, error) {
	if data, err := os.ReadFile(path); err == nil {
		var idx Index
		if json.Unmarshal(data, &idx) == nil && len(idx.Entries) > 0 && time.Since(idx.FetchedAt) < maxAge {
			return &idx, nil
		}
	}
	idx, err := c.FetchIndex(ctx)
	if err != nil {
		return nil, err
	}
	if data, err := json.Marshal(idx); err == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			_ = os.WriteFile(path, data, 0o644)
		}
	}
	return idx, nil
}

// CodeSnippet is the starter stub for one language.
type CodeSnippet struct {
	Lang     string `json:"lang"`
	LangSlug string `json:"langSlug"`
	Code     string `json:"code"`
}

// TopicTag is one category attached to a problem, e.g. "array" or "database".
type TopicTag struct {
	Slug string `json:"slug"`
}

// Question is the full detail of a problem.
type Question struct {
	QuestionID       string        `json:"questionId"`
	FrontendID       string        `json:"questionFrontendId"`
	Title            string        `json:"title"`
	TitleSlug        string        `json:"titleSlug"`
	Content          string        `json:"content"` // HTML
	Difficulty       string        `json:"difficulty"`
	IsPaidOnly       bool          `json:"isPaidOnly"`
	ExampleTestcases string        `json:"exampleTestcases"`
	MetaData         string        `json:"metaData"` // JSON string
	CodeSnippets     []CodeSnippet `json:"codeSnippets"`
	TopicTags        []TopicTag    `json:"topicTags"`
}

// Snippet returns the starter code for a language slug.
func (q *Question) Snippet(langSlug string) (string, bool) {
	for _, s := range q.CodeSnippets {
		if s.LangSlug == langSlug {
			return s.Code, true
		}
	}
	return "", false
}

// EntryPoint is the method name the judge calls (from metaData), e.g. "twoSum".
// Returns "" when metaData has no name, which is the case for database and
// shell problems.
func (q *Question) EntryPoint() string {
	var meta struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(q.MetaData), &meta) != nil {
		return ""
	}
	return meta.Name
}

const questionDataQuery = `query questionData($titleSlug: String!) {
  question(titleSlug: $titleSlug) {
    questionId
    questionFrontendId
    title
    titleSlug
    content
    difficulty
    isPaidOnly
    exampleTestcases
    metaData
    codeSnippets { lang langSlug code }
    topicTags { slug }
  }
}`

// GetQuestion fetches a problem's full detail by slug.
func (c *Client) GetQuestion(ctx context.Context, slug string) (*Question, error) {
	var data struct {
		Question *Question `json:"question"`
	}
	vars := map[string]any{"titleSlug": slug}
	if err := c.graphQL(ctx, "questionData", questionDataQuery, vars, &data); err != nil {
		return nil, err
	}
	if data.Question == nil {
		return nil, fmt.Errorf("problema %q não encontrado", slug)
	}
	if data.Question.Content == "" && data.Question.IsPaidOnly {
		return nil, fmt.Errorf("problema %q é premium-only", slug)
	}
	return data.Question, nil
}

var (
	htmlTag    = regexp.MustCompile(`(?s)<[^>]+>`)
	blankRuns  = regexp.MustCompile(`\n{3,}`)
	htmlEntity = strings.NewReplacer(
		"&nbsp;", " ", "&lt;", "<", "&gt;", ">", "&amp;", "&",
		"&quot;", `"`, "&#39;", "'", "&euro;", "€", "&ge;", "≥", "&le;", "≤",
	)
	blockBreaks = strings.NewReplacer(
		"</p>", "</p>\n", "<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</li>", "</li>\n", "</pre>", "</pre>\n", "</ul>", "</ul>\n",
	)
)

// PlainContent renders the HTML description as readable plain text.
func PlainContent(html string) string {
	s := blockBreaks.Replace(html)
	s = htmlTag.ReplaceAllString(s, "")
	s = htmlEntity.Replace(s)
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

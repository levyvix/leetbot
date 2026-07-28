package leetcode

import (
	"context"
	"regexp"
	"slices"
	"strings"
)

// SolutionArticle is one community solution post. topicId comes back from the
// API as a number, not a string.
type SolutionArticle struct {
	Title   string `json:"title"`
	TopicID int64  `json:"topicId"`
	Slug    string `json:"slug"`
	Summary string `json:"summary"`
	CanSee  bool   `json:"canSee"`
}

const solutionArticlesQuery = `query ugcArticleSolutionArticles(
  $questionSlug: String!
  $orderBy: ArticleOrderByEnum
  $tagSlugs: [String!]
  $skip: Int
  $first: Int
) {
  ugcArticleSolutionArticles(
    questionSlug: $questionSlug
    orderBy: $orderBy
    tagSlugs: $tagSlugs
    skip: $skip
    first: $first
  ) {
    totalNum
    edges { node { title topicId slug summary canSee } }
  }
}`

// ListSolutions returns the top community solutions for a problem, ordered by
// votes. langSlug filters by language tag; pass "" for no filter.
func (c *Client) ListSolutions(ctx context.Context, slug, langSlug string, first int) ([]SolutionArticle, error) {
	vars := map[string]any{
		"questionSlug": slug,
		"orderBy":      "MOST_VOTES",
		"skip":         0,
		"first":        first,
	}
	if langSlug != "" {
		vars["tagSlugs"] = []string{langSlug}
	}

	var data struct {
		Articles struct {
			Edges []struct {
				Node SolutionArticle `json:"node"`
			} `json:"edges"`
		} `json:"ugcArticleSolutionArticles"`
	}
	if err := c.graphQL(ctx, "ugcArticleSolutionArticles", solutionArticlesQuery, vars, &data); err != nil {
		return nil, err
	}

	out := make([]SolutionArticle, 0, len(data.Articles.Edges))
	for _, e := range data.Articles.Edges {
		if e.Node.CanSee && e.Node.TopicID != 0 {
			out = append(out, e.Node)
		}
	}
	return out, nil
}

const solutionDetailQuery = `query ugcArticleSolutionArticle($topicId: ID) {
  ugcArticleSolutionArticle(topicId: $topicId) {
    title
    content
  }
}`

// GetSolutionContent returns the markdown body of a solution post.
func (c *Client) GetSolutionContent(ctx context.Context, topicID int64) (string, error) {
	var data struct {
		Article struct {
			Content string `json:"content"`
		} `json:"ugcArticleSolutionArticle"`
	}
	vars := map[string]any{"topicId": topicID}
	if err := c.graphQL(ctx, "ugcArticleSolutionArticle", solutionDetailQuery, vars, &data); err != nil {
		return "", err
	}
	return data.Article.Content, nil
}

// langAliases maps a LeetCode language slug to the info strings people
// actually write on markdown fences.
var langAliases = map[string][]string{
	"python3":    {"python3", "python", "py"},
	"python":     {"python", "python3", "py"},
	"java":       {"java"},
	"cpp":        {"cpp", "c++", "cc"},
	"c":          {"c"},
	"csharp":     {"csharp", "c#", "cs"},
	"javascript": {"javascript", "js", "node"},
	"typescript": {"typescript", "ts"},
	"golang":     {"golang", "go"},
	"rust":       {"rust", "rs"},
	"kotlin":     {"kotlin", "kt"},
	"swift":      {"swift"},
	"ruby":       {"ruby", "rb"},
	"scala":      {"scala"},
	"php":        {"php"},
	"elixir":     {"elixir", "ex"},
	"dart":       {"dart"},
	"racket":     {"racket"},
	"erlang":     {"erlang", "erl"},
}

// fence matches a markdown code block, capturing the info string and the body.
var fence = regexp.MustCompile("(?s)```([^\n`]*)\n(.*?)```")

// unescapeContent handles solution posts whose body arrives with literal
// backslash-n sequences instead of real line breaks. Both encodings are in the
// wild, so it only rewrites bodies that have no real newline at all.
func unescapeContent(s string) string {
	if strings.Contains(s, "\n") || !strings.Contains(s, `\n`) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"', '\'', '\\':
			b.WriteByte(s[i])
		default:
			// Unknown escape: keep it verbatim so code is not corrupted.
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// ExtractCode pulls code blocks for langSlug out of a solution's markdown,
// keeping only those that define entryPoint. Blocks are returned in document
// order, which for a top-voted post puts the main solution first.
func ExtractCode(markdown, langSlug, entryPoint string) []string {
	markdown = unescapeContent(markdown)
	aliases := langAliases[langSlug]
	if aliases == nil {
		aliases = []string{langSlug}
	}

	var out []string
	for _, m := range fence.FindAllStringSubmatch(markdown, -1) {
		info := normalizeFenceInfo(m[1])
		body := strings.TrimSpace(m[2])
		if body == "" {
			continue
		}
		// An empty info string is common; accept it and let entryPoint decide.
		if info != "" && !slices.Contains(aliases, info) {
			continue
		}
		if entryPoint != "" && !strings.Contains(body, entryPoint) {
			continue
		}
		out = append(out, body)
	}
	return out
}

// normalizeFenceInfo turns "[Python3]", "python3 []", "Java" into a bare
// lowercase token.
func normalizeFenceInfo(info string) string {
	info = strings.ToLower(strings.TrimSpace(info))
	info = strings.Trim(info, "[]{}")
	if i := strings.IndexAny(info, " \t["); i >= 0 {
		info = info[:i]
	}
	return strings.TrimSpace(info)
}

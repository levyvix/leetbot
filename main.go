// Command leetbot fetches LeetCode problems, harvests top-voted community
// solutions, validates them against the example test cases and optionally
// submits them.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/levyvix/leetbot/internal/bot"
	"github.com/levyvix/leetbot/internal/leetcode"
)

const indexMaxAge = 24 * time.Hour

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `leetbot — bot de LeetCode baseado em soluções da comunidade

uso:
  leetbot whoami                    valida os cookies da sessão
  leetbot show <id|slug>            imprime a descrição do problema
  leetbot harvest <id|slug> [flags] lista as soluções candidatas (não executa nada)
  leetbot solve <id|slug> [flags]   resolve um problema
  leetbot run [flags]               resolve vários problemas em sequência
  leetbot stats [flags]             resumo do estado salvo

autenticação (variáveis de ambiente obrigatórias):
  LEETCODE_SESSION   cookie LEETCODE_SESSION do navegador
  LEETCODE_CSRF      cookie csrftoken do navegador

use "leetbot <comando> -h" para as flags de cada comando.
`)
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return fmt.Errorf("nenhum comando informado")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "whoami":
		return cmdWhoami(ctx)
	case "show":
		return cmdShow(ctx, os.Args[2:])
	case "harvest":
		return cmdHarvest(ctx, os.Args[2:])
	case "solve":
		return cmdSolve(ctx, os.Args[2:])
	case "run":
		return cmdRun(ctx, os.Args[2:])
	case "stats":
		return cmdStats(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("comando desconhecido: %q", os.Args[1])
	}
}

// parsePositional parses a flag set that takes exactly one positional
// argument, allowing flags on either side of it. Go's flag package stops at
// the first non-flag argument, so this resumes parsing after it.
func parsePositional(fs *flag.FlagSet, args []string, usage string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	var pos string
	for fs.NArg() > 0 {
		if pos != "" {
			return "", fmt.Errorf("argumento extra %q — uso: %s", fs.Arg(0), usage)
		}
		pos = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return "", err
		}
	}
	if pos == "" {
		return "", fmt.Errorf("uso: %s", usage)
	}
	return pos, nil
}

// newClient builds a client from the environment.
func newClient(interval time.Duration) (*leetcode.Client, error) {
	session := os.Getenv("LEETCODE_SESSION")
	csrf := os.Getenv("LEETCODE_CSRF")
	if session == "" || csrf == "" {
		return nil, fmt.Errorf("defina LEETCODE_SESSION e LEETCODE_CSRF (veja o README)")
	}
	return leetcode.New(session, csrf, interval), nil
}

func indexPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "problems.json"
	}
	return filepath.Join(dir, "leetbot", "problems.json")
}

func cmdWhoami(ctx context.Context) error {
	c, err := newClient(500 * time.Millisecond)
	if err != nil {
		return err
	}
	st, err := c.Whoami(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("autenticado como %s (premium: %t)\n", st.Username, st.IsPremium)
	return nil
}

func cmdShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	raw := fs.Bool("raw", false, "imprime o HTML original em vez de texto")
	lang := fs.String("lang", "", "também imprime o stub desta linguagem (ex: python3)")
	ref, err := parsePositional(fs, args, "leetbot show <id|slug> [flags]")
	if err != nil {
		return err
	}

	c, err := newClient(500 * time.Millisecond)
	if err != nil {
		return err
	}
	q, err := resolveQuestion(ctx, c, ref)
	if err != nil {
		return err
	}

	fmt.Printf("%s. %s [%s]\n%s/problems/%s/\n\n", q.FrontendID, q.Title, q.Difficulty, leetcode.BaseURL, q.TitleSlug)
	if *raw {
		fmt.Println(q.Content)
	} else {
		fmt.Println(leetcode.PlainContent(q.Content))
	}
	if *lang != "" {
		snippet, ok := q.Snippet(*lang)
		if !ok {
			return fmt.Errorf("sem stub para %q", *lang)
		}
		fmt.Printf("\n--- stub (%s) ---\n%s\n", *lang, snippet)
	}
	fmt.Printf("\n--- casos de exemplo ---\n%s\n", q.ExampleTestcases)
	return nil
}

// resolveQuestion accepts a frontend id or a slug.
func resolveQuestion(ctx context.Context, c *leetcode.Client, ref string) (*leetcode.Question, error) {
	slug := ref
	if _, err := fmt.Sscanf(ref, "%d", new(int)); err == nil {
		idx, err := c.LoadIndex(ctx, indexPath(), indexMaxAge)
		if err != nil {
			return nil, err
		}
		entry, ok := idx.Lookup(ref)
		if !ok {
			return nil, fmt.Errorf("nenhum problema com id %s", ref)
		}
		slug = entry.Slug
	}
	return c.GetQuestion(ctx, slug)
}

// cmdHarvest exercises the scraping stage in isolation: it lists candidate
// solutions without running or submitting anything.
func cmdHarvest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("harvest", flag.ExitOnError)
	lang := fs.String("lang", "python3", "slug da linguagem")
	articles := fs.Int("articles", 5, "quantos posts de solução ler")
	printFirst := fs.Bool("print", false, "imprime o primeiro candidato de cada post")
	ref, err := parsePositional(fs, args, "leetbot harvest <id|slug> [flags]")
	if err != nil {
		return err
	}

	c, err := newClient(500 * time.Millisecond)
	if err != nil {
		return err
	}
	q, err := resolveQuestion(ctx, c, ref)
	if err != nil {
		return err
	}
	entryPoint := q.EntryPoint()
	if entryPoint == "" {
		return fmt.Errorf("problema sem entry point (SQL/shell?)")
	}

	posts, err := c.ListSolutions(ctx, q.TitleSlug, *lang, *articles)
	if err != nil {
		return err
	}
	fmt.Printf("%s. %s | entry point: %s | %d posts em %s\n\n", q.FrontendID, q.Title, entryPoint, len(posts), *lang)

	total := 0
	for _, p := range posts {
		md, err := c.GetSolutionContent(ctx, p.TopicID)
		if err != nil {
			fmt.Printf("  [erro] %s: %v\n", p.Title, err)
			continue
		}
		blocks := leetcode.ExtractCode(md, *lang, entryPoint)
		total += len(blocks)
		fmt.Printf("  %d candidato(s) — %s\n", len(blocks), p.Title)
		if *printFirst && len(blocks) > 0 {
			fmt.Printf("%s\n\n", indent(blocks[0], "    "))
		}
	}
	fmt.Printf("\ntotal de candidatos: %d\n", total)
	if total == 0 {
		os.Exit(2)
	}
	return nil
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

func cmdSolve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("solve", flag.ExitOnError)
	cfg := bot.DefaultConfig()
	fs.StringVar(&cfg.Lang, "lang", cfg.Lang, "slug da linguagem (python3, java, cpp, golang, ...)")
	fs.BoolVar(&cfg.Submit, "submit", false, "submete de verdade (padrão: só testa nos exemplos)")
	fs.IntVar(&cfg.MaxArticles, "articles", cfg.MaxArticles, "quantos posts de solução ler")
	fs.IntVar(&cfg.MaxCandidates, "candidates", cfg.MaxCandidates, "quantos blocos de código testar")
	interval := fs.Duration("interval", 500*time.Millisecond, "intervalo mínimo entre requisições")
	printCode := fs.Bool("print", false, "imprime o código que passou")
	ref, err := parsePositional(fs, args, "leetbot solve <id|slug> [flags]")
	if err != nil {
		return err
	}

	c, err := newClient(*interval)
	if err != nil {
		return err
	}
	idx, err := c.LoadIndex(ctx, indexPath(), indexMaxAge)
	if err != nil {
		return err
	}
	entry, ok := idx.Lookup(ref)
	if !ok {
		return fmt.Errorf("problema %q não encontrado no índice", ref)
	}

	b := bot.New(c, cfg, logf)
	logf("%d. %s (%s)", entry.FrontendID, entry.Slug, cfg.Lang)
	out := b.Solve(ctx, *entry)
	fmt.Printf("\n%s: %s\n", out.Status, out.Detail)
	if *printCode && out.Code != "" {
		fmt.Printf("\n--- código ---\n%s\n", out.Code)
	}
	if out.Status == bot.StatusFailed || out.Status == bot.StatusError {
		os.Exit(2)
	}
	return nil
}

func cmdRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfg := bot.DefaultConfig()
	fs.StringVar(&cfg.Lang, "lang", cfg.Lang, "slug da linguagem")
	fs.BoolVar(&cfg.Submit, "submit", false, "submete de verdade (padrão: só testa nos exemplos)")
	fs.IntVar(&cfg.MaxArticles, "articles", cfg.MaxArticles, "quantos posts de solução ler por problema")
	fs.IntVar(&cfg.MaxCandidates, "candidates", cfg.MaxCandidates, "quantos blocos de código testar por problema")
	fs.DurationVar(&cfg.PauseBetween, "pause", cfg.PauseBetween, "pausa entre problemas")
	difficulty := fs.String("difficulty", "", "easy, medium, hard ou vazio para todas")
	limit := fs.Int("limit", 10, "número máximo de problemas nesta execução (0 = todos)")
	from := fs.Int("from", 0, "processa apenas problemas com id >= este valor")
	includeSolved := fs.Bool("include-solved", false, "não pula os problemas já resolvidos na conta")
	statePath := fs.String("state", "state.json", "arquivo de progresso")
	interval := fs.Duration("interval", 500*time.Millisecond, "intervalo mínimo entre requisições")
	_ = fs.Parse(args)

	c, err := newClient(*interval)
	if err != nil {
		return err
	}
	idx, err := c.LoadIndex(ctx, indexPath(), indexMaxAge)
	if err != nil {
		return err
	}
	state, err := bot.LoadState(*statePath)
	if err != nil {
		return err
	}

	wantLevel, err := parseDifficulty(*difficulty)
	if err != nil {
		return err
	}

	var queue []leetcode.IndexEntry
	for _, e := range idx.Entries {
		switch {
		case e.PaidOnly,
			e.FrontendID < *from,
			wantLevel != 0 && e.Difficulty != wantLevel,
			e.Solved && !*includeSolved:
			continue
		}
		queue = append(queue, e)
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].FrontendID < queue[j].FrontendID })
	if *limit > 0 && len(queue) > *limit {
		queue = queue[:*limit]
	}
	if len(queue) == 0 {
		return fmt.Errorf("nenhum problema corresponde aos filtros")
	}

	mode := "DRY-RUN (não submete)"
	if cfg.Submit {
		mode = "SUBMETENDO DE VERDADE"
	}
	logf("%d problemas na fila | %s | %s | pausa %s", len(queue), cfg.Lang, mode, cfg.PauseBetween)

	runErr := bot.New(c, cfg, logf).Run(ctx, queue, state)
	printTally(state)
	if runErr != nil && ctx.Err() != nil {
		logf("interrompido — progresso salvo em %s", *statePath)
		return nil
	}
	return runErr
}

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	statePath := fs.String("state", "state.json", "arquivo de progresso")
	_ = fs.Parse(args)

	state, err := bot.LoadState(*statePath)
	if err != nil {
		return err
	}
	printTally(state)
	return nil
}

func printTally(state *bot.State) {
	counts := state.Tally()
	order := []bot.Status{bot.StatusAccepted, bot.StatusTested, bot.StatusFailed, bot.StatusSkipped, bot.StatusError}
	total := 0
	var parts []string
	for _, s := range order {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", s, n))
			total += n
		}
	}
	if total == 0 {
		fmt.Println("\nnenhum problema processado ainda")
		return
	}
	fmt.Printf("\ntotal=%d %s\n", total, strings.Join(parts, " "))
}

func parseDifficulty(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return 0, nil
	case "easy":
		return leetcode.DiffEasy, nil
	case "medium":
		return leetcode.DiffMedium, nil
	case "hard":
		return leetcode.DiffHard, nil
	default:
		return 0, fmt.Errorf("dificuldade inválida: %q (use easy, medium ou hard)", s)
	}
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

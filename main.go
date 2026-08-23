// Command leetbot fetches LeetCode problems, harvests top-voted community
// solutions, validates them against the example test cases and optionally
// submits them.
package main

import (
	"context"
	"errors"
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
	"github.com/levyvix/leetbot/internal/dash"
	"github.com/levyvix/leetbot/internal/leetcode"
)

const indexMaxAge = 24 * time.Hour

// exitCode is an error that only carries a process exit status: the command
// has already reported what happened on stdout.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func main() {
	err := run()
	switch {
	case err == nil:
	case errors.Is(err, flag.ErrHelp):
		// The flag package already printed the usage.
		os.Exit(2)
	default:
		var code exitCode
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
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
  leetbot dash [flags]              painel ao vivo do progresso da conta

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
	case "dash":
		return cmdDash(ctx, os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("comando desconhecido: %q", os.Args[1])
	}
}

// newFlagSet returns a flag set that reports parse errors instead of calling
// os.Exit, so every command funnels its failures through run's error return.
func newFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
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
	c := leetcode.New(session, csrf, interval)
	c.OnCooldown = func(d time.Duration) {
		logf("429 do LeetCode — pausando %s antes de continuar", d.Round(time.Second))
	}
	return c, nil
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
	fs := newFlagSet("show")
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

// resolveQuestion accepts a frontend id or a slug. Only a ref that is entirely
// digits counts as an id — slugs such as "3sum" also start with one.
func resolveQuestion(ctx context.Context, c *leetcode.Client, ref string) (*leetcode.Question, error) {
	slug := strings.ToLower(strings.TrimSpace(ref))
	if _, isID := leetcode.IsID(ref); isID {
		idx, err := c.LoadIndex(ctx, indexPath(), indexMaxAge)
		if err != nil {
			return nil, err
		}
		entry, found := idx.Lookup(ref)
		if !found {
			return nil, fmt.Errorf("nenhum problema com id %s", ref)
		}
		slug = entry.Slug
	}
	return c.GetQuestion(ctx, slug)
}

// cmdHarvest exercises the scraping stage in isolation: it lists candidate
// solutions without running or submitting anything.
func cmdHarvest(ctx context.Context, args []string) error {
	fs := newFlagSet("harvest")
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
		return exitCode(2)
	}
	return nil
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

func cmdSolve(ctx context.Context, args []string) error {
	fs := newFlagSet("solve")
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
	out, solveErr := b.Solve(ctx, entry)
	fmt.Printf("\n%s: %s\n", out.Status, out.Detail)
	if *printCode && out.Code != "" {
		fmt.Printf("\n--- código ---\n%s\n", out.Code)
	}
	if solveErr != nil && !errors.Is(solveErr, context.Canceled) {
		return solveErr
	}
	if out.Status == bot.StatusFailed || out.Status == bot.StatusError {
		return exitCode(2)
	}
	return nil
}

func cmdRun(ctx context.Context, args []string) error {
	fs := newFlagSet("run")
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
	retryFailed := fs.Bool("retry-failed", false, "retenta apenas problemas salvos como failed")
	statePath := fs.String("state", "state.json", "arquivo de progresso")
	interval := fs.Duration("interval", 500*time.Millisecond, "intervalo mínimo entre requisições")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.RetryFailed = *retryFailed

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

	queue := filterQueue(idx.Entries, queueFilter{
		minID:         *from,
		difficulty:    wantLevel,
		includeSolved: *includeSolved,
	})
	if *retryFailed {
		queue = filterFailedQueue(queue, state)
	}
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
	if errors.Is(runErr, context.Canceled) {
		logf("interrompido — progresso salvo em %s", *statePath)
		return nil
	}
	if errors.Is(runErr, bot.ErrQuotaExhausted) {
		logf("progresso salvo em %s — rode o mesmo comando mais tarde para retomar", *statePath)
	}
	return runErr
}

// queueFilter selects which indexed problems are worth attempting.
type queueFilter struct {
	minID         int
	difficulty    int // 0 means any
	includeSolved bool
}

// filterQueue returns the matching entries sorted by frontend id.
func filterQueue(entries []leetcode.IndexEntry, f queueFilter) []leetcode.IndexEntry {
	var queue []leetcode.IndexEntry
	for _, e := range entries {
		if e.PaidOnly || e.FrontendID < f.minID {
			continue
		}
		if f.difficulty != 0 && e.Difficulty != f.difficulty {
			continue
		}
		if e.Solved && !f.includeSolved {
			continue
		}
		queue = append(queue, e)
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].FrontendID < queue[j].FrontendID })
	return queue
}

// filterFailedQueue keeps only indexed problems whose saved bot outcome is failed.
func filterFailedQueue(entries []leetcode.IndexEntry, state *bot.State) []leetcode.IndexEntry {
	queue := make([]leetcode.IndexEntry, 0, len(entries))
	for _, e := range entries {
		if outcome, ok := state.Get(e.Slug); ok && outcome.Status == bot.StatusFailed {
			queue = append(queue, e)
		}
	}
	return queue
}

func cmdStats(args []string) error {
	fs := newFlagSet("stats")
	statePath := fs.String("state", "state.json", "arquivo de progresso")
	if err := fs.Parse(args); err != nil {
		return err
	}

	state, err := bot.LoadState(*statePath)
	if err != nil {
		return err
	}
	printTally(state)
	return nil
}

func cmdDash(ctx context.Context, args []string) error {
	fs := newFlagSet("dash")
	user := fs.String("user", "", "username a consultar (padrão: a conta conectada)")
	statePath := fs.String("state", "state.json", "arquivo de progresso do bot")
	every := fs.Duration("every", 10*time.Second, "intervalo de atualização do painel")
	once := fs.Bool("once", false, "imprime uma vez e sai, em vez de ficar ao vivo")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *every < time.Second {
		return fmt.Errorf("-every mínimo é 1s (evita bater no rate limit)")
	}

	c, err := newClient(500 * time.Millisecond)
	if err != nil {
		return err
	}
	username := *user
	if username == "" {
		st, err := c.Whoami(ctx)
		if err != nil {
			return err
		}
		username = st.Username
	}
	if *once {
		stats, err := c.FetchAccountStats(ctx, username)
		if err != nil {
			return err
		}
		fmt.Print(dash.Render(stats, -1, botLine(*statePath), ""))
		return nil
	}

	baseline := -1
	tick := time.NewTicker(*every)
	defer tick.Stop()
	for {
		stats, err := c.FetchAccountStats(ctx, username)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if all, ok := stats.Bucket("All"); ok && baseline < 0 {
			baseline = all.Solved
		}
		footer := fmt.Sprintf("  %s  ·  atualiza a cada %s  ·  ctrl+c para sair\n",
			time.Now().Format("15:04:05"), *every)
		clear := ""
		if dash.IsTerminal(os.Stdout) {
			// Home + clear-below, so the frame redraws without flicker.
			clear = "\033[H\033[J"
		}
		fmt.Print(clear + dash.Render(stats, baseline, botLine(*statePath), footer))

		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// botLine summarises the bot's own progress for the dashboard, or "" when
// there is no readable state file.
func botLine(statePath string) string {
	state, err := bot.LoadState(statePath)
	if err != nil {
		return ""
	}
	line := tallyLine(state)
	if line == "" {
		return ""
	}
	return fmt.Sprintf("bot (%s)   %s", statePath, line)
}

func printTally(state *bot.State) {
	line := tallyLine(state)
	if line == "" {
		fmt.Println("\nnenhum problema processado ainda")
		return
	}
	fmt.Printf("\n%s\n", line)
}

// tallyLine renders the state counts as "total=N status=n ...", or "" when
// nothing was processed yet.
func tallyLine(state *bot.State) string {
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
		return ""
	}
	return fmt.Sprintf("total=%d %s", total, strings.Join(parts, " "))
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

package main

import (
	"testing"

	"github.com/levyvix/leetbot/internal/bot"
	"github.com/levyvix/leetbot/internal/leetcode"
)

func TestParseDifficulty(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"easy", leetcode.DiffEasy, false},
		{" Medium ", leetcode.DiffMedium, false},
		{"HARD", leetcode.DiffHard, false},
		{"impossible", 0, true},
	} {
		got, err := parseDifficulty(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseDifficulty(%q) err = %v, wantErr %t", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDifficulty(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestFilterQueue(t *testing.T) {
	entries := []leetcode.IndexEntry{
		{FrontendID: 30, Slug: "hard-one", Difficulty: leetcode.DiffHard},
		{FrontendID: 10, Slug: "easy-solved", Difficulty: leetcode.DiffEasy, Solved: true},
		{FrontendID: 20, Slug: "easy-paid", Difficulty: leetcode.DiffEasy, PaidOnly: true},
		{FrontendID: 5, Slug: "easy-open", Difficulty: leetcode.DiffEasy},
		{FrontendID: 40, Slug: "easy-late", Difficulty: leetcode.DiffEasy},
	}

	got := filterQueue(entries, queueFilter{})
	if want := []string{"easy-open", "hard-one", "easy-late"}; !slugsEqual(got, want) {
		t.Errorf("sem filtros = %v, want %v (ordenado por id, sem premium nem resolvidos)", slugs(got), want)
	}

	got = filterQueue(entries, queueFilter{difficulty: leetcode.DiffEasy})
	if want := []string{"easy-open", "easy-late"}; !slugsEqual(got, want) {
		t.Errorf("difficulty=easy = %v, want %v", slugs(got), want)
	}

	got = filterQueue(entries, queueFilter{minID: 30})
	if want := []string{"hard-one", "easy-late"}; !slugsEqual(got, want) {
		t.Errorf("minID=30 = %v, want %v", slugs(got), want)
	}

	got = filterQueue(entries, queueFilter{includeSolved: true, difficulty: leetcode.DiffEasy})
	if want := []string{"easy-open", "easy-solved", "easy-late"}; !slugsEqual(got, want) {
		t.Errorf("includeSolved = %v, want %v", slugs(got), want)
	}

	if q := filterQueue(nil, queueFilter{}); q != nil {
		t.Errorf("índice vazio = %v, want nil", q)
	}
}

func TestFilterFailedQueue(t *testing.T) {
	state := &bot.State{Outcomes: map[string]bot.Outcome{}}
	state.Put(bot.Outcome{Slug: "failed", Status: bot.StatusFailed})
	state.Put(bot.Outcome{Slug: "accepted", Status: bot.StatusAccepted})

	entries := []leetcode.IndexEntry{
		{Slug: "accepted"},
		{Slug: "failed"},
		{Slug: "unprocessed"},
	}
	got := filterFailedQueue(entries, state)
	if want := []string{"failed"}; !slugsEqual(got, want) {
		t.Errorf("failed queue = %v, want %v", slugs(got), want)
	}
}

func TestTallyLine(t *testing.T) {
	state := &bot.State{Outcomes: map[string]bot.Outcome{}}
	if got := tallyLine(state); got != "" {
		t.Errorf("estado vazio = %q, want string vazia", got)
	}

	state.Put(bot.Outcome{Slug: "a", Status: bot.StatusAccepted})
	state.Put(bot.Outcome{Slug: "b", Status: bot.StatusAccepted})
	state.Put(bot.Outcome{Slug: "c", Status: bot.StatusSkipped})

	want := "total=3 accepted=2 skipped=1"
	if got := tallyLine(state); got != want {
		t.Errorf("tallyLine() = %q, want %q", got, want)
	}
}

func slugs(entries []leetcode.IndexEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Slug
	}
	return out
}

func slugsEqual(entries []leetcode.IndexEntry, want []string) bool {
	got := slugs(entries)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

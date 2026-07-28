package dash

import (
	"strings"
	"testing"

	"github.com/levyvix/leetbot/internal/leetcode"
)

func TestBar(t *testing.T) {
	for _, tc := range []struct {
		name            string
		n, total, width int
		want            string
	}{
		{"vazio", 0, 10, 4, "[····]"},
		{"cheio", 10, 10, 4, "[████]"},
		{"metade", 5, 10, 4, "[██··]"},
		{"progresso mínimo visível", 1, 1000, 4, "[█···]"},
		{"total zero", 3, 0, 4, "[····]"},
		{"n acima do total não estoura", 12, 10, 4, "[████]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Bar(tc.n, tc.total, tc.width); got != tc.want {
				t.Errorf("Bar(%d, %d, %d) = %q, want %q", tc.n, tc.total, tc.width, got, tc.want)
			}
		})
	}
}

func TestPct(t *testing.T) {
	if got := Pct(0, 0); got != 0 {
		t.Errorf("Pct(0, 0) = %v, want 0", got)
	}
	if got := Pct(1, 4); got != 25 {
		t.Errorf("Pct(1, 4) = %v, want 25", got)
	}
}

func TestThousands(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{5000000, "5,000,000"},
		{-12345, "-12,345"},
	} {
		if got := Thousands(tc.in); got != tc.want {
			t.Errorf("Thousands(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRender(t *testing.T) {
	stats := &leetcode.AccountStats{
		Username:         "alice",
		Ranking:          123456,
		TotalSubmissions: 40,
		Buckets: []leetcode.DifficultyStat{
			{Difficulty: "All", Solved: 30, Total: 3000, Submissions: 35},
			{Difficulty: "Easy", Solved: 20, Total: 800, Beats: 61.5},
		},
	}
	got := Render(stats, 25, "bot (state.json)   total=2 accepted=2", "rodapé\n")

	for _, want := range []string{"alice", "rank #123,456", "+5 nesta sessão", "Total", "Easy", "beats 61.5%", "bot (state.json)", "rodapé"} {
		if !strings.Contains(got, want) {
			t.Errorf("frame não contém %q:\n%s", want, got)
		}
	}
	// Hard não veio na resposta, então não deve aparecer.
	if strings.Contains(got, "Hard") {
		t.Errorf("frame mostra bucket ausente:\n%s", got)
	}
	// baseline == -1 omite o delta da sessão.
	if strings.Contains(Render(stats, -1, "", ""), "nesta sessão") {
		t.Error("baseline -1 não deveria render o delta da sessão")
	}
}

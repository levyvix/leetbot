// Package dash renders the account progress panel.
package dash

import (
	"fmt"
	"os"
	"strings"

	"github.com/levyvix/leetbot/internal/leetcode"
)

const (
	barWidth  = 22
	ruleWidth = 46
)

// Render builds one frame. baseline is the solved count at the start of a live
// session (-1 to omit the delta), botLine an optional summary of the bot's own
// progress, and footer is appended at the end.
func Render(stats *leetcode.AccountStats, baseline int, botLine, footer string) string {
	var b strings.Builder
	all, _ := stats.Bucket("All")

	fmt.Fprintf(&b, "\n  %s", stats.Username)
	if stats.Ranking > 0 {
		fmt.Fprintf(&b, "  ·  rank #%s", Thousands(stats.Ranking))
	}
	if baseline >= 0 && all.Solved > baseline {
		fmt.Fprintf(&b, "  ·  +%d nesta sessão", all.Solved-baseline)
	}
	fmt.Fprintf(&b, "\n  %s\n\n", strings.Repeat("─", ruleWidth))

	for _, name := range []string{"All", "Easy", "Medium", "Hard"} {
		d, ok := stats.Bucket(name)
		if !ok {
			continue
		}
		label := name
		if name == "All" {
			label = "Total"
		}
		fmt.Fprintf(&b, "  %-7s %s %4d/%-5d %5.1f%%", label, Bar(d.Solved, d.Total, barWidth), d.Solved, d.Total, Pct(d.Solved, d.Total))
		if d.Beats > 0 {
			fmt.Fprintf(&b, "   beats %.1f%%", d.Beats)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "\n  %s\n", strings.Repeat("─", ruleWidth))
	fmt.Fprintf(&b, "  submissões       %d aceitas / %d totais (%.1f%%)\n", all.Submissions, stats.TotalSubmissions, Pct(all.Submissions, stats.TotalSubmissions))
	fmt.Fprintf(&b, "  dias ativos      %d  ·  streak atual %d\n", stats.TotalActiveDays, stats.Streak)
	if stats.Reputation > 0 {
		fmt.Fprintf(&b, "  reputação        %d\n", stats.Reputation)
	}
	if botLine != "" {
		fmt.Fprintf(&b, "  %s\n", botLine)
	}
	b.WriteString("\n" + footer)
	return b.String()
}

// Bar renders a proportional progress bar of the given width. n is clamped to
// total, which the API can exceed when its buckets disagree.
func Bar(n, total, width int) string {
	filled := 0
	if total > 0 {
		filled = min(n*width/total, width)
		if filled == 0 && n > 0 {
			filled = 1
		}
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("·", width-filled) + "]"
}

// Pct is n as a percentage of total, and 0 when total is 0.
func Pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) * 100 / float64(total)
}

// Thousands formats an int with "," separators, e.g. 5000000 -> "5,000,000".
func Thousands(n int) string {
	s := fmt.Sprint(n)
	end := 0
	if strings.HasPrefix(s, "-") {
		end = 1
	}
	for i := len(s) - 3; i > end; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// IsTerminal reports whether f is a character device, i.e. safe to move the
// cursor around with escape sequences.
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

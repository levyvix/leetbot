package leetcode

import (
	"context"
	"fmt"
)

// DifficultyStat is the progress on one difficulty bucket.
type DifficultyStat struct {
	Difficulty  string // "All", "Easy", "Medium", "Hard"
	Solved      int
	Total       int
	Submissions int     // accepted submissions (>= Solved, counts retries)
	Beats       float64 // percentage of users beaten
}

// AccountStats is everything the dashboard shows about one account.
type AccountStats struct {
	Username         string
	Ranking          int
	Reputation       int
	Streak           int
	TotalActiveDays  int
	TotalSubmissions int // every submission, accepted or not
	Buckets          []DifficultyStat
}

// Bucket returns the stat for a difficulty name.
func (s *AccountStats) Bucket(difficulty string) (DifficultyStat, bool) {
	for _, b := range s.Buckets {
		if b.Difficulty == difficulty {
			return b, true
		}
	}
	return DifficultyStat{}, false
}

const accountStatsQuery = `query userProfile($username: String!) {
  allQuestionsCount { difficulty count }
  matchedUser(username: $username) {
    username
    profile { ranking reputation }
    userCalendar { streak totalActiveDays }
    problemsSolvedBeatsStats { difficulty percentage }
    submitStatsGlobal {
      acSubmissionNum { difficulty count submissions }
      totalSubmissionNum { difficulty submissions }
    }
  }
}`

// FetchAccountStats returns the solved counts and profile numbers for a user.
func (c *Client) FetchAccountStats(ctx context.Context, username string) (*AccountStats, error) {
	var data struct {
		AllQuestionsCount []struct {
			Difficulty string `json:"difficulty"`
			Count      int    `json:"count"`
		} `json:"allQuestionsCount"`
		MatchedUser *struct {
			Username string `json:"username"`
			Profile  struct {
				Ranking    int `json:"ranking"`
				Reputation int `json:"reputation"`
			} `json:"profile"`
			UserCalendar struct {
				Streak          int `json:"streak"`
				TotalActiveDays int `json:"totalActiveDays"`
			} `json:"userCalendar"`
			ProblemsSolvedBeatsStats []struct {
				Difficulty string   `json:"difficulty"`
				Percentage *float64 `json:"percentage"`
			} `json:"problemsSolvedBeatsStats"`
			SubmitStatsGlobal struct {
				AcSubmissionNum []struct {
					Difficulty  string `json:"difficulty"`
					Count       int    `json:"count"`
					Submissions int    `json:"submissions"`
				} `json:"acSubmissionNum"`
				TotalSubmissionNum []struct {
					Difficulty  string `json:"difficulty"`
					Submissions int    `json:"submissions"`
				} `json:"totalSubmissionNum"`
			} `json:"submitStatsGlobal"`
		} `json:"matchedUser"`
	}
	vars := map[string]any{"username": username}
	if err := c.graphQL(ctx, "userProfile", accountStatsQuery, vars, &data); err != nil {
		return nil, err
	}
	if data.MatchedUser == nil {
		return nil, fmt.Errorf("usuário %q não encontrado", username)
	}
	u := data.MatchedUser

	totals := map[string]int{}
	for _, q := range data.AllQuestionsCount {
		totals[q.Difficulty] = q.Count
	}
	beats := map[string]float64{}
	for _, b := range u.ProblemsSolvedBeatsStats {
		if b.Percentage != nil {
			beats[b.Difficulty] = *b.Percentage
		}
	}

	stats := &AccountStats{
		Username:        u.Username,
		Ranking:         u.Profile.Ranking,
		Reputation:      u.Profile.Reputation,
		Streak:          u.UserCalendar.Streak,
		TotalActiveDays: u.UserCalendar.TotalActiveDays,
	}
	for _, ac := range u.SubmitStatsGlobal.AcSubmissionNum {
		stats.Buckets = append(stats.Buckets, DifficultyStat{
			Difficulty:  ac.Difficulty,
			Solved:      ac.Count,
			Total:       totals[ac.Difficulty],
			Submissions: ac.Submissions,
			Beats:       beats[ac.Difficulty],
		})
	}
	for _, t := range u.SubmitStatsGlobal.TotalSubmissionNum {
		if t.Difficulty == "All" {
			stats.TotalSubmissions = t.Submissions
		}
	}
	return stats, nil
}

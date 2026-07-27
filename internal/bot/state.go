package bot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// State is a resumable record of every problem already processed.
type State struct {
	path string

	mu       sync.Mutex
	Outcomes map[string]Outcome `json:"outcomes"`
}

// LoadState reads state from path, starting empty when the file is absent.
func LoadState(path string) (*State, error) {
	s := &State{path: path, Outcomes: map[string]Outcome{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	if s.Outcomes == nil {
		s.Outcomes = map[string]Outcome{}
	}
	return s, nil
}

// Get returns a previously recorded outcome.
func (s *State) Get(slug string) (Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.Outcomes[slug]
	return o, ok
}

// Put records an outcome.
func (s *State) Put(o Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Outcomes[o.Slug] = o
}

// Save atomically writes the state to disk.
func (s *State) Save() error {
	s.mu.Lock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Tally counts outcomes by status.
func (s *State) Tally() map[Status]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := map[Status]int{}
	for _, o := range s.Outcomes {
		counts[o.Status]++
	}
	return counts
}

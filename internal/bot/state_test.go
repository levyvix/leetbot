package bot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStateMissingFile(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), "nao-existe.json"))
	if err != nil {
		t.Fatalf("LoadState de arquivo ausente: %v", err)
	}
	if len(s.Outcomes) != 0 {
		t.Errorf("estado inicial não está vazio: %+v", s.Outcomes)
	}
	// Um Put logo em seguida não pode entrar em pânico por mapa nil.
	s.Put(Outcome{Slug: "two-sum", Status: StatusTested})
	if _, ok := s.Get("two-sum"); !ok {
		t.Error("Put/Get não persistiu em memória")
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")

	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Outcome{
		Slug:       "two-sum",
		FrontendID: 1,
		Status:     StatusAccepted,
		Detail:     "Accepted | 40 ms | 17 MB",
		Code:       "class Solution: ...",
		Attempts:   2,
		At:         time.Now().UTC().Truncate(time.Second),
	}
	s.Put(want)
	if err := s.Save(); err != nil {
		t.Fatalf("Save criando diretório: %v", err)
	}

	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("two-sum")
	if !ok {
		t.Fatal("outcome não sobreviveu ao round-trip")
	}
	if !got.At.Equal(want.At) {
		t.Errorf("At = %v, want %v", got.At, want.At)
	}
	got.At, want.At = time.Time{}, time.Time{}
	if got != want {
		t.Errorf("outcome = %+v, want %+v", got, want)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Put(Outcome{Slug: "a", Status: StatusTested})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("arquivo temporário não foi renomeado: %s", e.Name())
		}
	}
}

func TestLoadStateCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{isso não é json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Error("LoadState devia falhar em JSON inválido em vez de descartar o progresso")
	}
}

func TestTally(t *testing.T) {
	s := &State{Outcomes: map[string]Outcome{}}
	s.Put(Outcome{Slug: "a", Status: StatusAccepted})
	s.Put(Outcome{Slug: "b", Status: StatusAccepted})
	s.Put(Outcome{Slug: "c", Status: StatusFailed})
	// Mesmo slug: sobrescreve, não soma.
	s.Put(Outcome{Slug: "c", Status: StatusSkipped})

	got := s.Tally()
	want := map[Status]int{StatusAccepted: 2, StatusSkipped: 1}
	if len(got) != len(want) {
		t.Fatalf("Tally() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Tally()[%s] = %d, want %d", k, got[k], v)
		}
	}
}

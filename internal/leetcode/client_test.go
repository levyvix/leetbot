package leetcode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testClient points a client at srv with negligible throttling and backoff, so
// retry behaviour can be exercised without real delays.
func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := New("sess", "tok", 0)
	c.baseURL = srv.URL
	c.backoff = time.Millisecond
	return c
}

func TestDoRetriesOnServerError(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})

	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.getJSON(t.Context(), "/x", "", &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if !out.OK {
		t.Error("resposta não foi decodificada")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("%d chamadas, want 3", got)
	}
}

func TestDoGivesUpAfterAttempts(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})

	if err := c.getJSON(t.Context(), "/x", "", nil); err == nil {
		t.Fatal("getJSON devia falhar após esgotar as tentativas")
	}
	if got := calls.Load(); got != defaultAttempts {
		t.Errorf("%d chamadas, want %d", got, defaultAttempts)
	}
}

func TestDoUnauthorizedIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	})

	err := c.getJSON(t.Context(), "/x", "", nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("%d chamadas, want 1 (cookie morto não melhora com retry)", got)
	}
}

func TestSubmitCodeIsNotRetried(t *testing.T) {
	var submits atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		submits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})

	q := &Question{TitleSlug: "two-sum", QuestionID: "1"}
	if _, err := c.SubmitCode(t.Context(), q, "python3", "code"); err == nil {
		t.Fatal("SubmitCode devia propagar o 502")
	}
	if got := submits.Load(); got != 1 {
		t.Errorf("%d submissões, want 1 — o retry duplicaria a tentativa na conta", got)
	}
}

func TestRunCodeIsRetried(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case calls.Add(1) == 1:
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/problems/two-sum/interpret_solution/":
			w.Write([]byte(`{"interpret_id":"abc"}`))
		default:
			w.Write([]byte(`{"state":"SUCCESS","run_success":true,"correct_answer":true}`))
		}
	})

	q := &Question{TitleSlug: "two-sum", QuestionID: "1"}
	res, err := c.RunCode(t.Context(), q, "python3", "code", "input")
	if err != nil {
		t.Fatalf("RunCode: %v", err)
	}
	if !res.CorrectAnswer {
		t.Error("veredito não foi decodificado")
	}
}

func TestThrottleRespectsContext(t *testing.T) {
	c := New("sess", "tok", time.Hour)
	c.last = time.Now()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan error, 1)
	go func() { done <- c.throttle(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("throttle err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("throttle ignorou o cancelamento do contexto")
	}
}

func TestGraphQLSurfacesErrors(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":null,"errors":[{"message":"que problema?"}]}`))
	})

	err := c.graphQL(t.Context(), "op", "query {}", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "que problema?") {
		t.Errorf("err = %v, want a mensagem do GraphQL", err)
	}
}

func TestWhoamiRejectsSignedOut(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"userStatus":{"username":"","isSignedIn":false}}}`))
	})

	_, err := c.Whoami(t.Context())
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

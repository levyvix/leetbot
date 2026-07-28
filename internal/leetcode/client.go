// Package leetcode is a thin client for LeetCode's internal (undocumented) API.
package leetcode

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	BaseURL   = "https://leetcode.com"
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// ErrUnauthorized reports that the session cookies are missing or expired.
// Every request will keep failing until they are renewed, so callers running a
// batch should stop instead of retrying the next item.
var ErrUnauthorized = errors.New("sessão inválida ou expirada — renove os cookies")

// Client talks to leetcode.com using the browser session cookies.
type Client struct {
	http     *http.Client
	baseURL  string
	session  string
	csrf     string
	interval time.Duration
	backoff  time.Duration // unit of the exponential retry delay

	mu   sync.Mutex
	last time.Time
}

// New builds a client. session and csrf come from the browser cookies
// LEETCODE_SESSION and csrftoken. interval is the minimum delay between
// requests.
func New(session, csrf string, interval time.Duration) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				// LeetCode's edge behaves better over HTTP/1.1.
				TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:  BaseURL,
		session:  session,
		csrf:     csrf,
		interval: interval,
		backoff:  time.Second,
	}
}

// throttle blocks until at least interval has passed since the last request,
// or until ctx is cancelled.
func (c *Client) throttle(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.interval - time.Since(c.last); wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	c.last = time.Now()
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path, referer string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return nil, err
	}
	if referer == "" {
		referer = c.baseURL + "/"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", referer)
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("x-requested-with", "XMLHttpRequest")
	req.Header.Set("x-csrftoken", c.csrf)
	req.Header.Set("Cookie", fmt.Sprintf("LEETCODE_SESSION=%s; csrftoken=%s", c.session, c.csrf))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// defaultAttempts is how many times an idempotent request is tried before
// giving up. Non-idempotent endpoints must pass 1.
const defaultAttempts = 3

// do sends the request up to attempts times, retrying on 429 and 5xx, and
// decodes JSON into out.
func (c *Client) do(ctx context.Context, method, path, referer string, body []byte, out any, attempts int) error {
	var lastErr error

	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * c.backoff):
			}
		}
		if err := c.throttle(ctx); err != nil {
			return err
		}

		req, err := c.newRequest(ctx, method, path, referer, body)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("%s %s: %w", method, path, err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("%s %s: lendo resposta: %w", method, path, err)
			continue
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("%s %s: %s", method, path, resp.Status)
			continue
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
			return fmt.Errorf("%s %s: %s: %w", method, path, resp.Status, ErrUnauthorized)
		case resp.StatusCode >= 300:
			return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, truncate(string(data), 300))
		}

		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: resposta não é JSON: %s", method, path, truncate(string(data), 300))
		}
		return nil
	}
	return lastErr
}

func (c *Client) getJSON(ctx context.Context, path, referer string, out any) error {
	return c.do(ctx, http.MethodGet, path, referer, nil, out, defaultAttempts)
}

func (c *Client) postJSON(ctx context.Context, path, referer string, in, out any) error {
	return c.postJSONAttempts(ctx, path, referer, in, out, defaultAttempts)
}

// postJSONAttempts is postJSON with an explicit retry budget. Endpoints that
// mutate account state pass 1: a retry after a 5xx could duplicate an action
// the server already performed.
func (c *Client) postJSONAttempts(ctx context.Context, path, referer string, in, out any, attempts int) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, referer, body, out, attempts)
}

type graphQLError struct {
	Message string `json:"message"`
}

// graphQL runs a query and unmarshals the "data" object into out.
func (c *Client) graphQL(ctx context.Context, op, query string, vars map[string]any, out any) error {
	req := map[string]any{"operationName": op, "query": query, "variables": vars}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := c.postJSON(ctx, "/graphql/", "", req, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql %s: %s", op, strings.Join(msgs, "; "))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

// UserStatus is the currently authenticated user.
type UserStatus struct {
	Username   string `json:"username"`
	IsSignedIn bool   `json:"isSignedIn"`
	IsPremium  bool   `json:"isPremium"`
}

const userStatusQuery = `query globalData {
  userStatus { username isSignedIn isPremium }
}`

// Whoami verifies the session cookies.
func (c *Client) Whoami(ctx context.Context) (*UserStatus, error) {
	var data struct {
		UserStatus UserStatus `json:"userStatus"`
	}
	if err := c.graphQL(ctx, "globalData", userStatusQuery, nil, &data); err != nil {
		return nil, err
	}
	if !data.UserStatus.IsSignedIn {
		return nil, fmt.Errorf("não autenticado: %w", ErrUnauthorized)
	}
	return &data.UserStatus, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

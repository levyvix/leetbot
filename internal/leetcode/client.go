// Package leetcode is a thin client for LeetCode's internal (undocumented) API.
package leetcode

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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

// Client talks to leetcode.com using the browser session cookies.
type Client struct {
	http     *http.Client
	session  string
	csrf     string
	interval time.Duration

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
		session:  session,
		csrf:     csrf,
		interval: interval,
	}
}

// throttle blocks until at least interval has passed since the last request.
func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.interval - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func (c *Client) newRequest(ctx context.Context, method, path, referer string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	if referer == "" {
		referer = BaseURL + "/"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", referer)
	req.Header.Set("Origin", BaseURL)
	req.Header.Set("x-requested-with", "XMLHttpRequest")
	req.Header.Set("x-csrftoken", c.csrf)
	req.Header.Set("Cookie", fmt.Sprintf("LEETCODE_SESSION=%s; csrftoken=%s", c.session, c.csrf))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// do sends the request, retrying on 429 and 5xx, and decodes JSON into out.
func (c *Client) do(ctx context.Context, method, path, referer string, body []byte, out any) error {
	const attempts = 3
	var lastErr error

	for attempt := range attempts {
		if attempt > 0 {
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		c.throttle()

		req, err := c.newRequest(ctx, method, path, referer, body)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("%s %s: %s", method, path, resp.Status)
			continue
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
			return fmt.Errorf("%s %s: %s (sessão inválida ou expirada — renove os cookies)", method, path, resp.Status)
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
	return c.do(ctx, http.MethodGet, path, referer, nil, out)
}

func (c *Client) postJSON(ctx context.Context, path, referer string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, referer, body, out)
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
		return nil, fmt.Errorf("não autenticado: cookies ausentes ou expirados")
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

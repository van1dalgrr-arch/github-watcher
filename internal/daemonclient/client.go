// Package daemonclient — как TUI и CLI разговаривают с `gw daemon` по HTTP.
package daemonclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// ErrOffline — демон не запущен (или недоступен по сети).
var ErrOffline = errors.New("демон не запущен — запусти `gw daemon`")

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) Status(ctx context.Context) (watcher.Status, error) {
	var s watcher.Status
	return s, c.get(ctx, "/api/status", &s)
}

func (c *Client) Events(ctx context.Context, repo string, limit int) ([]domain.Event, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if repo != "" {
		q.Set("repo", repo)
	}
	var out []domain.Event
	return out, c.get(ctx, "/api/events?"+q.Encode(), &out)
}

func (c *Client) Logs(ctx context.Context) ([]string, error) {
	var out []string
	return out, c.get(ctx, "/api/logs", &out)
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrOffline
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("демон ответил HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

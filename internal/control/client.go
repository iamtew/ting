package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func NewClient(base, token string) *Client {
	return &Client{
		Base:  strings.TrimRight(base, "/"),
		Token: token,
		HTTP:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Status() (Status, error) {
	var st Status
	err := c.do(http.MethodGet, "/status", nil, &st)
	return st, err
}

func (c *Client) Join(channel string) error {
	return c.do(http.MethodPost, "/join", map[string]string{"channel": channel}, nil)
}

func (c *Client) Part(channel, reason string) error {
	return c.do(http.MethodPost, "/part", map[string]string{"channel": channel, "reason": reason}, nil)
}

func (c *Client) Privmsg(target, text string) error {
	return c.do(http.MethodPost, "/privmsg", map[string]string{"target": target, "text": text}, nil)
}

func (c *Client) Nick(nick string) error {
	return c.do(http.MethodPost, "/nick", map[string]string{"nick": nick}, nil)
}

func (c *Client) Raw(line string) error {
	return c.do(http.MethodPost, "/raw", map[string]string{"line": line}, nil)
}

func (c *Client) Quit(reason string) error {
	return c.do(http.MethodPost, "/quit", map[string]string{"reason": reason}, nil)
}

func (c *Client) SyncChannels(channels []string) error {
	if channels == nil {
		channels = []string{}
	}
	return c.do(http.MethodPost, "/channels", map[string][]string{"channels": channels}, nil)
}

func (c *Client) Shutdown() error {
	return c.do(http.MethodPost, "/shutdown", struct{}{}, nil)
}

func (c *Client) Events(ctx context.Context) (<-chan Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	hc := &http.Client{Timeout: 0}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("events: %s", res.Status)
	}
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
		}
	}()
	return out, nil
}

func (c *Client) do(method, path string, body any, dst any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("%s %s: %s %s", method, path, res.Status, bytes.TrimSpace(b))
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(dst)
}

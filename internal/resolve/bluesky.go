package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Public Bluesky AppView (unauthenticated app.bsky.* GETs). Fragile if Bluesky
// moves the host or changes getPostThread shape — Meat Bags: no API key needed.
var bskyAppView = "https://public.api.bsky.app"

var bskyHost = regexp.MustCompile(`(?i)^(www\.)?bsky\.app$`)
var bskyPath = regexp.MustCompile(`(?i)^/profile/([^/]+)/post/([a-z0-9]+)$`)

// Bluesky resolves bsky.app post URLs via the public AppView.
type Bluesky struct {
	client *http.Client
	ua     string
}

// Match detects bsky.app /profile/.../post/... URLs.
func (b *Bluesky) Match(u *url.URL) bool {
	if u == nil {
		return false
	}
	if !bskyHost.MatchString(u.Hostname()) {
		return false
	}
	return bskyPath.MatchString(u.Path)
}

// Minimal getPostThread JSON — only fields we print for Meat Bags on IRC.
type bskyThreadResp struct {
	Thread *struct {
		Type string `json:"$type"`
		Post *struct {
			Author struct {
				Handle string `json:"handle"`
			} `json:"author"`
			Record struct {
				Text      string `json:"text"`
				CreatedAt string `json:"createdAt"`
			} `json:"record"`
			ReplyCount  int `json:"replyCount"`
			RepostCount int `json:"repostCount"`
			LikeCount   int `json:"likeCount"`
			Embed       *struct {
				Images []struct {
					Fullsize string `json:"fullsize"`
				} `json:"images"`
				Playlist string `json:"playlist"`
			} `json:"embed"`
		} `json:"post"`
	} `json:"thread"`
}

// Resolve fetches post text and stats.
func (b *Bluesky) Resolve(ctx context.Context, u *url.URL) (string, bool, error) {
	m := bskyPath.FindStringSubmatch(u.Path)
	if len(m) < 3 {
		return "", false, nil
	}
	actor, rkey := m[1], m[2]
	atURI := fmt.Sprintf("at://%s/app.bsky.feed.post/%s", actor, rkey)
	apiURL := bskyAppView + "/xrpc/app.bsky.feed.getPostThread?" +
		url.Values{"uri": {atURI}, "depth": {"0"}, "parentHeight": {"0"}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", b.ua)

	resp, err := b.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", false, err
	}

	var parsed bskyThreadResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, err
	}
	th := parsed.Thread
	if th == nil || th.Post == nil || !strings.Contains(th.Type, "threadViewPost") {
		return "", false, nil
	}
	p := th.Post
	text := collapseSpace(p.Record.Text)
	if len(text) > 180 {
		text = text[:177] + "..."
	}
	ts := p.Record.CreatedAt
	if tstamp, err := time.Parse(time.RFC3339Nano, p.Record.CreatedAt); err == nil {
		ts = tstamp.UTC().Format(time.RFC3339)
	}
	author := p.Author.Handle
	if author == "" {
		author = actor
	}

	var media []string
	if emb := p.Embed; emb != nil {
		for _, img := range emb.Images {
			if img.Fullsize != "" {
				media = append(media, img.Fullsize)
			}
		}
		if emb.Playlist != "" {
			media = append(media, emb.Playlist)
		}
	}

	out := fmt.Sprintf("@%s: %s | %s | RT:%d replies:%d likes:%d",
		author, text, ts, p.RepostCount, p.ReplyCount, p.LikeCount)
	if len(media) > 0 {
		out += " | media: " + strings.Join(media, " ")
	}
	return out, true, nil
}

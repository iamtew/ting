package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Arctic Shift is a public Reddit archive API (no OAuth). Prefer it over Reddit
// HTML/.json — those often 403 from datacenter IPs after Reddit shut down
// unauthenticated JSON (2026). Fragile: archive lag on brand-new posts; host/path
// shape can change. oEmbed is a thin Reddit fallback (title/author only; also
// IP-sensitive). Public downvote counts are not exposed (API downs is always 0).
const (
	arcticShiftPostsURL = "https://arctic-shift.photon-reddit.com/api/posts/ids"
	redditOEmbedURL     = "https://www.reddit.com/oembed"
)

var (
	redditHost      = regexp.MustCompile(`(?i)^(www\.|old\.|np\.|m\.)?reddit\.com$`)
	redditShortHost = regexp.MustCompile(`(?i)^(www\.)?redd\.it$`)
	// /r/sub/comments/<id>/… or /comments/<id>/…
	redditCommentsPath = regexp.MustCompile(`(?i)^/(?:r/[^/]+/)?comments/([a-z0-9]+)(?:/|$)`)
	redditShortPath    = regexp.MustCompile(`(?i)^/([a-z0-9]+)(?:/|$)`)
	redditSubFromPath  = regexp.MustCompile(`(?i)^/r/([^/]+)(?:/|$)`)
)

// Reddit resolves submission URLs via Arctic Shift, with Reddit oEmbed fallback.
type Reddit struct {
	client *http.Client
	ua     string
}

// Match detects reddit.com / redd.it submission URLs with a post id.
func (r *Reddit) Match(u *url.URL) bool {
	return redditPostID(u) != ""
}

func redditPostID(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if redditShortHost.MatchString(host) {
		m := redditShortPath.FindStringSubmatch(u.Path)
		if len(m) == 2 {
			return strings.ToLower(m[1])
		}
		return ""
	}
	if !redditHost.MatchString(host) {
		return ""
	}
	m := redditCommentsPath.FindStringSubmatch(u.Path)
	if len(m) == 2 {
		return strings.ToLower(m[1])
	}
	return ""
}

func redditSubredditFromURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	m := redditSubFromPath.FindStringSubmatch(u.Path)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

type arcticPostsResp struct {
	Data []arcticPost `json:"data"`
}

type arcticPost struct {
	Title       string  `json:"title"`
	Author      string  `json:"author"`
	Subreddit   string  `json:"subreddit"`
	Score       int     `json:"score"`
	NumComments int     `json:"num_comments"`
	CreatedUTC  float64 `json:"created_utc"`
}

type redditOEmbedResp struct {
	Title      string `json:"title"`
	AuthorName string `json:"author_name"`
}

// Resolve fetches post metadata (Arctic Shift first, oEmbed fallback).
func (r *Reddit) Resolve(ctx context.Context, u *url.URL) (string, bool, error) {
	id := redditPostID(u)
	if id == "" {
		return "", false, nil
	}
	if reply, ok, _ := r.fromArctic(ctx, id, u); ok {
		return reply, true, nil
	}
	return r.fromOEmbed(ctx, u)
}

func (r *Reddit) fromArctic(ctx context.Context, id string, src *url.URL) (string, bool, error) {
	apiURL := arcticShiftPostsURL + "?ids=" + url.QueryEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", false, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("arctic shift: http %d", resp.StatusCode)
	}

	var parsed arcticPostsResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, err
	}
	if len(parsed.Data) == 0 {
		return "", false, nil
	}
	p := parsed.Data[0]
	title := collapseSpace(p.Title)
	if title == "" {
		return "", false, nil
	}

	sub := strings.TrimSpace(p.Subreddit)
	if sub == "" {
		sub = redditSubredditFromURL(src)
	}
	author := strings.TrimSpace(p.Author)

	parts := []string{"Reddit: " + title}
	if sub != "" {
		parts = append(parts, "r/"+sub)
	}
	if author != "" && !strings.EqualFold(author, "[deleted]") {
		parts = append(parts, "u/"+author)
	}
	parts = append(parts, formatRedditScore(p.Score))
	if d := formatUnixDate(p.CreatedUTC); d != "" {
		parts = append(parts, d)
	}
	if p.NumComments > 0 {
		parts = append(parts, formatRedditComments(p.NumComments))
	}
	return strings.Join(parts, " | "), true, nil
}

func (r *Reddit) fromOEmbed(ctx context.Context, u *url.URL) (string, bool, error) {
	// Canonicalise to www.reddit.com for oEmbed; short/old hosts still work when
	// passed as-is, but a stable https URL avoids odd redirects.
	embedURL := u.String()
	if redditShortHost.MatchString(u.Hostname()) {
		id := redditPostID(u)
		if id != "" {
			embedURL = "https://www.reddit.com/comments/" + id + "/"
		}
	} else if host := strings.ToLower(u.Hostname()); strings.HasSuffix(host, "reddit.com") {
		nu := *u
		nu.Scheme = "https"
		nu.Host = "www.reddit.com"
		embedURL = nu.String()
	}

	apiURL := redditOEmbedURL + "?url=" + url.QueryEscape(embedURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", false, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("reddit oembed: http %d", resp.StatusCode)
	}

	var parsed redditOEmbedResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, err
	}
	title := collapseSpace(html.UnescapeString(parsed.Title))
	if title == "" {
		return "", false, nil
	}
	author := strings.TrimSpace(parsed.AuthorName)
	sub := redditSubredditFromURL(u)

	parts := []string{"Reddit: " + title}
	if sub != "" {
		parts = append(parts, "r/"+sub)
	}
	if author != "" {
		parts = append(parts, "u/"+author)
	}
	return strings.Join(parts, " | "), true, nil
}

func formatRedditScore(n int) string {
	return compactCount(strconv.Itoa(n)) + " score"
}

func formatRedditComments(n int) string {
	c := compactCount(strconv.Itoa(n))
	if n == 1 {
		return c + " comment"
	}
	return c + " comments"
}

func formatUnixDate(sec float64) string {
	if sec <= 0 {
		return ""
	}
	t := time.Unix(int64(sec), 0).UTC()
	return t.Format("2006-01-02")
}

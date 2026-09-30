package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ytHost  = regexp.MustCompile(`(?i)^(www\.|m\.|music\.)?(youtube\.com|youtu\.be)$`)
	ytWatch = regexp.MustCompile(`(?i)(?:v=|/embed/|/shorts/|/live/|/v/)([A-Za-z0-9_-]{6,})`)
	ytBe    = regexp.MustCompile(`(?i)^/([A-Za-z0-9_-]{6,})`)

	isoDurationRe = regexp.MustCompile(`^P(?:(\d+)D)?T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)
)

// youtubeAPIBase is Google Data API v3 videos.list (1 quota unit).
const youtubeAPIBase = "https://www.googleapis.com/youtube/v3/videos"

// YouTube resolves watch URLs via Data API v3 when a Meat Bag API key is set.
type YouTube struct {
	client *http.Client
	ua     string
	key    string
}

// Match detects youtube.com / youtu.be video URLs.
func (y *YouTube) Match(u *url.URL) bool {
	if u == nil || !ytHost.MatchString(u.Hostname()) {
		return false
	}
	return youtubeID(u) != ""
}

func youtubeID(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if strings.HasSuffix(host, "youtu.be") {
		m := ytBe.FindStringSubmatch(u.Path)
		if len(m) == 2 {
			return m[1]
		}
		return ""
	}
	if v := u.Query().Get("v"); v != "" {
		return v
	}
	m := ytWatch.FindStringSubmatch(u.Path)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

type ytAPIResp struct {
	Items []struct {
		Snippet struct {
			Title        string `json:"title"`
			ChannelTitle string `json:"channelTitle"`
			PublishedAt  string `json:"publishedAt"`
		} `json:"snippet"`
		ContentDetails struct {
			Duration string `json:"duration"`
		} `json:"contentDetails"`
		Statistics struct {
			ViewCount string `json:"viewCount"`
			LikeCount string `json:"likeCount"`
		} `json:"statistics"`
	} `json:"items"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Resolve fetches snippet + duration + stats from Data API v3.
// Never log y.key or the request URL (the key is in the query).
func (y *YouTube) Resolve(ctx context.Context, u *url.URL) (string, bool, error) {
	id := youtubeID(u)
	if id == "" || strings.TrimSpace(y.key) == "" {
		return "", false, nil
	}

	q := url.Values{}
	q.Set("part", "snippet,contentDetails,statistics")
	q.Set("id", id)
	q.Set("key", y.key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, youtubeAPIBase+"?"+q.Encode(), nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", y.ua)

	resp, err := y.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", false, err
	}

	var parsed ytAPIResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, err
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", false, fmt.Errorf("youtube api: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("youtube api: http %d", resp.StatusCode)
	}
	if len(parsed.Items) == 0 {
		return "", false, nil
	}
	it := parsed.Items[0]
	title := collapseSpace(it.Snippet.Title)
	if title == "" {
		return "", false, nil
	}

	parts := []string{"YouTube: " + title}
	if ch := collapseSpace(it.Snippet.ChannelTitle); ch != "" {
		parts = append(parts, ch)
	}
	if d := formatISODuration(it.ContentDetails.Duration); d != "" {
		parts = append(parts, d)
	}
	if uploaded := formatUploadDate(it.Snippet.PublishedAt); uploaded != "" {
		parts = append(parts, "uploaded "+uploaded)
	}
	if v := compactCount(it.Statistics.ViewCount); v != "" {
		parts = append(parts, v+" views")
	}
	if l := compactCount(it.Statistics.LikeCount); l != "" {
		parts = append(parts, l+" likes")
	}
	return strings.Join(parts, " | "), true, nil
}

func formatSeconds(sec int) string {
	if sec <= 0 {
		return ""
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatISODuration(raw string) string {
	raw = strings.TrimSpace(raw)
	m := isoDurationRe.FindStringSubmatch(raw)
	if len(m) != 5 {
		return ""
	}
	days, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	min, _ := strconv.Atoi(m[3])
	s, _ := strconv.Atoi(m[4])
	return formatSeconds(days*86400 + h*3600 + min*60 + s)
}

func formatUploadDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	formats := []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02"}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	if len(raw) >= 10 && raw[4] == '-' && raw[7] == '-' {
		return raw[:10]
	}
	return raw
}

// compactCount turns "415854353" into "415.9M" for IRC-friendly lines.
func compactCount(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if raw == "" {
		return ""
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n < 0 {
		return raw
	}
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", n/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", n/1_000)
	default:
		return strconv.FormatInt(int64(n), 10)
	}
}

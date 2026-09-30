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

// FxTwitter-style status API (no OAuth in v1).
const fxTwitterBase = "https://api.fxtwitter.com"

var twitterHost = regexp.MustCompile(`(?i)^(www\.|mobile\.)?(twitter\.com|x\.com)$`)
var twitterPath = regexp.MustCompile(`(?i)^/([^/]+)/status(?:es)?/(\d+)`)

// Twitter resolves X/Twitter status URLs via FxTwitter JSON.
type Twitter struct {
	client *http.Client
	ua     string
}

// Match detects twitter.com / x.com status URLs.
func (t *Twitter) Match(u *url.URL) bool {
	if u == nil {
		return false
	}
	if !twitterHost.MatchString(u.Hostname()) {
		return false
	}
	return twitterPath.MatchString(u.Path)
}

type fxResp struct {
	Code  int `json:"code"`
	Tweet *struct {
		Text    string `json:"text"`
		Created string `json:"created_at"`
		Author  struct {
			Name       string `json:"name"`
			ScreenName string `json:"screen_name"`
		} `json:"author"`
		Replies  int `json:"replies"`
		Retweets int `json:"retweets"`
		Likes    int `json:"likes"`
		Media    *struct {
			Photos []struct {
				URL string `json:"url"`
			} `json:"photos"`
			Videos []struct {
				URL string `json:"url"`
			} `json:"videos"`
			All []struct {
				URL string `json:"url"`
			} `json:"all"`
		} `json:"media"`
	} `json:"tweet"`
}

// Resolve fetches tweet text and stats.
func (t *Twitter) Resolve(ctx context.Context, u *url.URL) (string, bool, error) {
	m := twitterPath.FindStringSubmatch(u.Path)
	if len(m) < 3 {
		return "", false, nil
	}
	screen, id := m[1], m[2]
	apiURL := fmt.Sprintf("%s/%s/status/%s", fxTwitterBase, url.PathEscape(screen), id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", t.ua)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", false, err
	}

	var parsed fxResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, err
	}
	if parsed.Code != 200 || parsed.Tweet == nil {
		return "", false, nil
	}
	tw := parsed.Tweet
	text := collapseSpace(tw.Text)
	if len(text) > 180 {
		text = text[:177] + "..."
	}
	ts := tw.Created
	if tstamp, err := time.Parse(time.RubyDate, tw.Created); err == nil {
		ts = tstamp.UTC().Format(time.RFC3339)
	}
	author := tw.Author.ScreenName
	if author == "" {
		author = tw.Author.Name
	}

	var media []string
	if tw.Media != nil {
		for _, p := range tw.Media.Photos {
			if p.URL != "" {
				media = append(media, p.URL)
			}
		}
		for _, v := range tw.Media.Videos {
			if v.URL != "" {
				media = append(media, v.URL)
			}
		}
		if len(media) == 0 {
			for _, a := range tw.Media.All {
				if a.URL != "" {
					media = append(media, a.URL)
				}
			}
		}
	}

	out := fmt.Sprintf("@%s: %s | %s | RT:%d replies:%d likes:%d",
		author, text, ts, tw.Retweets, tw.Replies, tw.Likes)
	if len(media) > 0 {
		out += " | media: " + strings.Join(media, " ")
	}
	return out, true, nil
}

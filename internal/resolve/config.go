package resolve

import (
	"strings"
	"time"
)

// Config is per-server resolver toggles (t3b [resolve]). Nil bools mean on.
type Config struct {
	URLTitles      *bool
	Twitter        *bool
	Bluesky        *bool
	YouTube        *bool
	YouTubeAPIKey  string
	Reddit         *bool
	UserAgent      string
	HTTPTimeoutSec int
}

func (c Config) URLTitlesOn() bool { return c.URLTitles == nil || *c.URLTitles }
func (c Config) TwitterOn() bool   { return c.Twitter == nil || *c.Twitter }
func (c Config) BlueskyOn() bool   { return c.Bluesky == nil || *c.Bluesky }
func (c Config) YouTubeOn() bool   { return c.YouTube == nil || *c.YouTube }
func (c Config) RedditOn() bool    { return c.Reddit == nil || *c.Reddit }

func (c Config) HTTPTimeout() time.Duration {
	sec := c.HTTPTimeoutSec
	if sec <= 0 {
		sec = 8
	}
	return time.Duration(sec) * time.Second
}

func (c Config) agent() string {
	if s := strings.TrimSpace(c.UserAgent); s != "" {
		return s
	}
	return "Mozilla/5.0 (compatible; ting; +https://github.com/iamtew/ting)"
}

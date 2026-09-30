// Package resolve fetches titles / tweet / Bluesky / YouTube / Reddit metadata for channel URLs.
package resolve

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// MaxIRCLine keeps PRIVMSG replies IRC-safe.
const MaxIRCLine = 350

// urlRe finds http(s) URLs conservatively (no spaces / angle brackets).
var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// Resolver turns one URL into a channel reply line.
type Resolver interface {
	// Match reports whether this resolver handles the URL.
	Match(u *url.URL) bool
	// Resolve returns reply text; ok=false means silent skip; err is logged by caller.
	Resolve(ctx context.Context, u *url.URL) (reply string, ok bool, err error)
}

// Engine picks the first URL in a message and runs the matching resolver.
type Engine struct {
	log       *log.Logger
	cfg       Config
	client    *http.Client
	resolvers []Resolver

	mu        sync.Mutex
	rate      map[string][]time.Time // channel -> resolution timestamps
	maxPerMin int
}

// New builds an Engine from config.
func New(logger *log.Logger, cfg Config) *Engine {
	if logger == nil {
		logger = log.Default()
	}
	e := &Engine{
		log:       logger,
		cfg:       cfg,
		client:    &http.Client{Timeout: cfg.HTTPTimeout(), CheckRedirect: limitRedirects(5)},
		rate:      make(map[string][]time.Time),
		maxPerMin: 6,
	}
	e.rebuild()
	return e
}

func limitRedirects(n int) func(*http.Request, []*http.Request) error {
	return func(_ *http.Request, via []*http.Request) error {
		if len(via) >= n {
			return http.ErrUseLastResponse
		}
		return nil
	}
}

// UpdateConfig refreshes toggles / UA / timeout after reload.
func (e *Engine) UpdateConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
	e.client.Timeout = cfg.HTTPTimeout()
	e.rebuildLocked()
}

func (e *Engine) rebuild() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rebuildLocked()
}

func (e *Engine) rebuildLocked() {
	e.resolvers = nil
	// Order: specialised handlers before generic title so they win on their hosts.
	ua := e.cfg.agent()
	if e.cfg.TwitterOn() {
		e.resolvers = append(e.resolvers, &Twitter{client: e.client, ua: ua})
	}
	if e.cfg.BlueskyOn() {
		e.resolvers = append(e.resolvers, &Bluesky{client: e.client, ua: ua})
	}
	// YouTube Data API only when a key is set; otherwise URLTitle handles the link.
	if e.cfg.YouTubeOn() {
		if key := strings.TrimSpace(e.cfg.YouTubeAPIKey); key != "" {
			e.resolvers = append(e.resolvers, &YouTube{client: e.client, ua: ua, key: key})
		}
	}
	if e.cfg.RedditOn() {
		e.resolvers = append(e.resolvers, &Reddit{client: e.client, ua: ua})
	}
	if e.cfg.URLTitlesOn() {
		e.resolvers = append(e.resolvers, &URLTitle{client: e.client, ua: ua})
	}
}

// HandleMessage resolves the first URL in text for a channel. Empty reply = nothing to say.
func (e *Engine) HandleMessage(ctx context.Context, channel, text string) string {
	raw := FirstURL(text)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		e.log.Printf("resolve skip %s: bad URL", raw)
		return ""
	}
	if !e.allow(channel) {
		return ""
	}

	e.mu.Lock()
	resolvers := append([]Resolver(nil), e.resolvers...)
	e.mu.Unlock()

	matched := false
	for _, r := range resolvers {
		if !r.Match(u) {
			continue
		}
		matched = true
		reply, ok, err := r.Resolve(ctx, u)
		if err != nil {
			e.log.Printf("resolve %s: %v", raw, err)
			return ""
		}
		if !ok || reply == "" {
			e.log.Printf("resolve skip %s: no title", raw)
			return ""
		}
		return TrimIRC(reply)
	}
	if !matched {
		e.log.Printf("resolve skip %s: no matching resolver", raw)
	}
	return ""
}

func (e *Engine) allow(channel string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	stamps := e.rate[channel]
	kept := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= e.maxPerMin {
		e.rate[channel] = kept
		return false
	}
	e.rate[channel] = append(kept, now)
	return true
}

// FirstURL returns the first http(s) URL in text, trimming trailing punctuation.
// IRC formatting codes and zero-width junk are stripped first so inline links
// wrapped by clients (underline, color, bold) still match.
func FirstURL(text string) string {
	m := urlRe.FindString(stripIRCFormatting(text))
	if m == "" {
		return ""
	}
	return strings.TrimRight(m, ".,);]!?")
}

// stripIRCFormatting removes mIRC/IRC attribute bytes and invisible paste junk
// so URL extraction sees the same bytes Meat Bags intend to share.
func stripIRCFormatting(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case 0x02, 0x0f, 0x16, 0x1d, 0x1e, 0x1f, 0x11: // bold, reset, reverse, italic, strike, underline, monospace
			i++
			continue
		case 0x03: // color: \x03[[fg][,bg]] with 1–2 digit codes
			i++
			i += ircColorDigits(s[i:])
			if i < len(s) && s[i] == ',' {
				i++
				i += ircColorDigits(s[i:])
			}
			continue
		case 0x04: // hex color (Irssi / modern clients): drop code + following hex run
			i++
			for i < len(s) && isHexByte(s[i]) {
				i++
			}
			if i < len(s) && s[i] == ',' {
				i++
				for i < len(s) && isHexByte(s[i]) {
					i++
				}
			}
			continue
		}
		// Drop BOM / zero-width chars that hitch a ride on pasted links.
		if c == 0xef && i+2 < len(s) && s[i+1] == 0xbb && s[i+2] == 0xbf { // UTF-8 BOM
			i += 3
			continue
		}
		if c == 0xe2 && i+2 < len(s) {
			// U+200B ZWSP, U+200C ZWNJ, U+200D ZWJ, U+2060 WJ (UTF-8 e2 80 8b–8d / e2 81 a0)
			b2, b3 := s[i+1], s[i+2]
			if b2 == 0x80 && (b3 == 0x8b || b3 == 0x8c || b3 == 0x8d) {
				i += 3
				continue
			}
			if b2 == 0x81 && b3 == 0xa0 {
				i += 3
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// ircColorDigits consumes up to two ASCII digits for an IRC color code.
func ircColorDigits(s string) int {
	n := 0
	for n < len(s) && n < 2 && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// TrimIRC truncates a reply to MaxIRCLine runes-ish (byte-safe cut).
func TrimIRC(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) <= MaxIRCLine {
		return s
	}
	return s[:MaxIRCLine-3] + "..."
}

package resolve

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxBody = 1 << 20 // 1 MiB

// Link-preview UA that some consent walls (e.g. DPG Media) allow through.
// ponytail: allowlists change; widen detection or drop retry if it stops working.
const linkPreviewUA = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"

// URLTitle fetches HTML and extracts <title>.
type URLTitle struct {
	client *http.Client
	ua     string
}

// Match accepts any http(s) URL (used after specialised resolvers).
func (u *URLTitle) Match(raw *url.URL) bool {
	return raw != nil && (raw.Scheme == "http" || raw.Scheme == "https")
}

// Resolve GETs the page and returns its title.
// Cookie/consent gates, challenge pages, empty titles, and 403s are not
// announced; one retry uses a link-preview UA that often receives the real HTML.
func (u *URLTitle) Resolve(ctx context.Context, raw *url.URL) (string, bool, error) {
	title, final, err := u.fetchTitle(ctx, raw, u.ua)
	if shouldRetryTitle(err, final, title) {
		title, final, err = u.fetchTitle(ctx, raw, linkPreviewUA)
	}
	if err != nil {
		return "", false, err
	}
	if title == "" || isJunkTitle(final, title) {
		return "", false, nil
	}
	return fmt.Sprintf("Title: %s", title), true, nil
}

func shouldRetryTitle(err error, final *url.URL, title string) bool {
	if isHTTPStatus(err, http.StatusForbidden) {
		return true
	}
	if err != nil {
		return false
	}
	return title == "" || isJunkTitle(final, title)
}

func isHTTPStatus(err error, code int) bool {
	return err != nil && err.Error() == fmt.Sprintf("HTTP %d", code)
}

func (u *URLTitle) fetchTitle(ctx context.Context, raw *url.URL, ua string) (string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw.String(), nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html")

	resp, err := u.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	final := resp.Request.URL

	// Non-2xx often means a WAF/CDN error page with a misleading <title>
	// (e.g. CloudFront "Technical Difficulties"). Do not announce those.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return "", final, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "html") && !strings.Contains(ct, "text/") && !strings.Contains(ct, "xml") {
		return "", final, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", final, err
	}
	return extractTitle(string(body)), final, nil
}

// isJunkTitle is CMP / bot-challenge / archive interstitial text — never announce.
func isJunkTitle(final *url.URL, title string) bool {
	if final != nil {
		h := strings.ToLower(final.Hostname())
		if strings.Contains(h, "myprivacy.") {
			return true
		}
	}
	t := strings.ToLower(strings.TrimSpace(title))
	switch t {
	case "archive.ph", "archive.is", "archive.today":
		return true
	}
	return strings.Contains(t, "privacy gate") ||
		strings.Contains(t, "cookie wall") ||
		strings.Contains(t, "cookie consent") ||
		strings.Contains(t, "just a moment") ||
		strings.Contains(t, "attention required") ||
		strings.Contains(t, "access denied")
}

func extractTitle(raw string) string {
	lower := strings.ToLower(raw)
	if t := metaContent(raw, lower, "og:title"); t != "" {
		return t
	}
	if t := metaContent(raw, lower, "twitter:title"); t != "" {
		return t
	}
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	tagEnd := strings.Index(lower[start:], ">")
	if tagEnd < 0 {
		return ""
	}
	contentStart := start + tagEnd + 1
	endRel := strings.Index(lower[contentStart:], "</title>")
	if endRel < 0 {
		return ""
	}
	return cleanTitle(raw[contentStart : contentStart+endRel])
}

// metaContent reads content= from a <meta property|name="key"> tag (either attr order).
func metaContent(raw, lower, key string) string {
	needles := []string{
		`property="` + key + `"`,
		`property='` + key + `'`,
		`name="` + key + `"`,
		`name='` + key + `'`,
	}
	for _, n := range needles {
		i := strings.Index(lower, n)
		if i < 0 {
			continue
		}
		lt := strings.LastIndex(lower[:i+len(n)], "<")
		if lt < 0 {
			continue
		}
		gtRel := strings.Index(lower[lt:], ">")
		if gtRel < 0 {
			continue
		}
		if v := htmlAttr(raw[lt:lt+gtRel], lower[lt:lt+gtRel], "content"); v != "" {
			return cleanTitle(v)
		}
	}
	return ""
}

func htmlAttr(raw, lower, name string) string {
	needles := []string{name + `="`, name + `='`}
	for _, n := range needles {
		i := strings.Index(lower, n)
		if i < 0 {
			continue
		}
		q := raw[i+len(n)-1]
		rest := raw[i+len(n):]
		end := strings.IndexByte(rest, q)
		if end < 0 {
			continue
		}
		return rest[:end]
	}
	return ""
}

func cleanTitle(s string) string {
	t := collapseSpace(html.UnescapeString(s))
	if !utf8.ValidString(t) {
		t = strings.ToValidUTF8(t, "")
	}
	return t
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

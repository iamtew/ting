package master

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/iamtew/ting/internal/store"
)

const linkHelp = "link search: .link / .l | .link search <q> / .l s <q> | .link <id> | .link last [n] / .l l | .more / .m"

type linkSession struct {
	entries []store.Link
	offset  int
}

type linkPager struct {
	mu sync.Mutex
	s  map[string]linkSession
}

func newLinkPager() *linkPager {
	return &linkPager{s: make(map[string]linkSession)}
}

func pagerKey(serverID int64, dest, nick string) string {
	return strconv.FormatInt(serverID, 10) + "\x00" + dest + "\x00" + nick
}

func linkReplies(d *store.DB, p *linkPager, serverID int64, dest, nick, name string, args []string) []string {
	if d == nil {
		return []string{"link log unavailable"}
	}
	key := pagerKey(serverID, dest, nick)
	switch name {
	case "more", "m":
		return linkMore(p, key)
	case "link", "l":
		return linkCmd(d, p, key, serverID, args)
	default:
		return nil
	}
}

func linkCmd(d *store.DB, p *linkPager, key string, serverID int64, args []string) []string {
	if len(args) == 0 {
		st, err := d.LinkStats(serverID)
		if err != nil {
			return []string{"link log unavailable: " + err.Error()}
		}
		return []string{fmt.Sprintf("%d links, %d domains. %s", st.Total, st.Domains, linkHelp)}
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "search", "s":
		q := strings.Join(args[1:], " ")
		if strings.TrimSpace(q) == "" {
			return []string{"usage: .link search <q>"}
		}
		hits, err := d.SearchLinks(serverID, q)
		if err != nil {
			return []string{"link search failed: " + err.Error()}
		}
		if len(hits) == 0 {
			return []string{"no matches"}
		}
		return startPage(p, key, hits)
	case "last", "l":
		n := 3
		if len(args) > 1 {
			v, err := strconv.Atoi(args[1])
			if err != nil || v < 1 {
				return []string{"usage: .link last [n]"}
			}
			n = v
		}
		hits, err := d.LastLinks(serverID, n)
		if err != nil {
			return []string{"link last failed: " + err.Error()}
		}
		if len(hits) == 0 {
			return []string{"no links yet"}
		}
		return startPage(p, key, hits)
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return []string{linkHelp}
	}
	e, ok, err := d.GetLink(serverID, id)
	if err != nil {
		return []string{"link lookup failed: " + err.Error()}
	}
	if !ok {
		return []string{fmt.Sprintf("no link #%d", id)}
	}
	return []string{store.FormatEntry(e)}
}

func startPage(p *linkPager, key string, hits []store.Link) []string {
	lines, next, _ := store.FormatPage(hits, 0)
	p.mu.Lock()
	p.s[key] = linkSession{entries: hits, offset: next}
	p.mu.Unlock()
	return lines
}

func linkMore(p *linkPager, key string) []string {
	p.mu.Lock()
	sess, ok := p.s[key]
	p.mu.Unlock()
	if !ok || len(sess.entries) == 0 {
		return []string{"nothing to continue — .link last or .link search <q> first"}
	}
	lines, next, more := store.FormatPage(sess.entries, sess.offset)
	if len(lines) == 0 {
		return []string{"end of results"}
	}
	p.mu.Lock()
	if !more {
		delete(p.s, key)
	} else {
		sess.offset = next
		p.s[key] = sess
	}
	p.mu.Unlock()
	return lines
}

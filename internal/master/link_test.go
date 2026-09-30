package master

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/iamtew/ting/internal/store"
)

func TestLinkRepliesPublic(t *testing.T) {
	d, err := store.Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(store.Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	p := newLinkPager()
	got := linkReplies(d, p, b.ID, "#ting", "bob", "link", nil)
	if len(got) != 1 || !strings.Contains(got[0], "0 links") || !strings.Contains(got[0], ".link search") {
		t.Fatalf("empty: %q", got)
	}
	if err := d.InsertLink(b.ID, "#ting", "bob", "https://example.com/a", "Title: alpha"); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertLink(b.ID, "#ting", "bob", "https://example.com/b", "Title: beta"); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertLink(b.ID, "#lab", "ann", "https://other.test/x", "Title: gamma"); err != nil {
		t.Fatal(err)
	}

	got = linkReplies(d, p, b.ID, "#ting", "bob", "l", nil)
	if len(got) != 1 || !strings.Contains(got[0], "3 links, 2 domains") {
		t.Fatalf("stats: %q", got)
	}

	got = linkReplies(d, p, b.ID, "#ting", "bob", "link", []string{"search", "example.com"})
	if len(got) != 3 || !strings.Contains(got[0], "alpha") || !strings.Contains(got[2], "Showing 1-2 of 2") {
		t.Fatalf("search: %q", got)
	}

	got = linkReplies(d, p, b.ID, "#ting", "bob", "link", []string{"last", "5"})
	if len(got) != 4 || !strings.Contains(got[0], "gamma") || !strings.Contains(got[3], "Showing 1-3 of 3") {
		t.Fatalf("last: %q", got)
	}
	got = linkReplies(d, p, b.ID, "#ting", "bob", "m", nil)
	if len(got) != 1 || got[0] != "end of results" {
		t.Fatalf("more: %q", got)
	}

	hits, err := d.LastLinks(b.ID, 2)
	if err != nil || len(hits) != 2 {
		t.Fatalf("lastlinks %+v %v", hits, err)
	}
	got = linkReplies(d, p, b.ID, "#ting", "bob", "l", []string{"l", "2"})
	if len(got) != 3 || !strings.Contains(got[0], hits[0].URL) {
		t.Fatalf("last2: %q", got)
	}

	one := linkReplies(d, p, b.ID, "#ting", "bob", "link", []string{strconv.FormatInt(hits[0].ID, 10)})
	if len(one) != 1 || !strings.Contains(one[0], hits[0].URL) {
		t.Fatalf("byid: %q", one)
	}

	if Allowed("", "link") || Allowed("", "more") {
		t.Fatal("ACL dispatch must not own public link")
	}
}

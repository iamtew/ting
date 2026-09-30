package master

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamtew/ting/internal/karma"
	"github.com/iamtew/ting/internal/store"
)

func TestKarmaLookupPublic(t *testing.T) {
	d, err := store.Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(store.Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := karmaLookup(d, b.ID, nil); got != karma.HelpText() {
		t.Fatalf("help: %q", got)
	}
	if got := karmaLookup(d, b.ID, []string{"pizza"}); got != "pizza: 0 (unknown)" {
		t.Fatalf("unknown: %q", got)
	}
	if _, _, err := d.AddKarma(b.ID, "pizza", 4); err != nil {
		t.Fatal(err)
	}
	if got := karmaLookup(d, b.ID, []string{"pizza"}); got != "pizza: 4" {
		t.Fatalf("found: %q", got)
	}
	if Allowed("", "karma") {
		t.Fatal("ACL dispatch must not own public karma")
	}
}

func TestKarmaBumpReplies(t *testing.T) {
	d, err := store.Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(store.Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}

	lines, handled, err := karmaBumpReplies(d, b.ID, "ting", "you", "hello world")
	if err != nil || handled || lines != nil {
		t.Fatalf("plain: handled=%v lines=%v err=%v", handled, lines, err)
	}

	lines, handled, err = karmaBumpReplies(d, b.ID, "ting", "you", "pizza++")
	if err != nil || !handled || len(lines) != 0 {
		t.Fatalf("silent: %+v %v %v", lines, handled, err)
	}
	score, found, err := d.GetKarma(b.ID, "pizza")
	if err != nil || !found || score != 1 {
		t.Fatalf("score=%d found=%v", score, found)
	}

	lines, handled, err = karmaBumpReplies(d, b.ID, "ting", "you", "nope+1..24")
	if err != nil || !handled || len(lines) != 1 || !strings.Contains(lines[0], "hakuna yer tata's") {
		t.Fatalf("reject: %+v %v %v", lines, handled, err)
	}

	lines, handled, err = karmaBumpReplies(d, b.ID, "ting", "bob", "ting++")
	if err != nil || !handled || len(lines) != 3 {
		t.Fatalf("self: %+v %v %v", lines, handled, err)
	}
	if !strings.Contains(lines[0], "bob") || lines[1] != "ting+2..20" {
		t.Fatalf("self lines %+v", lines)
	}
}

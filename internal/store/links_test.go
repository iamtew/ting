package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndImportT3BLinks(t *testing.T) {
	raw := `
{"id":1,"datetime":"2024-01-02T03:04:05Z","channel":"#ting","user":"bob","domain":"example.com","URL":"https://example.com/a","title":"Title: hello"}
not json
{"id":2,"datetime":"2024-01-02T03:04:06Z","channel":"#ting","user":"bob","URL":"https://example.com/b","title":"Title: two"}

{"id":3,"URL":"","title":"nope"}
`
	got, skipped, err := ParseT3BLinks(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 || len(got) != 2 {
		t.Fatalf("n=%d skip=%d", len(got), skipped)
	}
	if got[1].Domain != "example.com" {
		t.Fatalf("domain %q", got[1].Domain)
	}

	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	st, err := d.ImportLinks(b.ID, got)
	if err != nil {
		t.Fatal(err)
	}
	if st.Imported != 2 || st.Skipped != 0 || st.Total != 2 {
		t.Fatalf("%+v", st)
	}
	st2, err := d.ImportLinks(b.ID, got)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Imported != 0 || st2.Duplicates != 2 || st2.Total != 2 {
		t.Fatalf("reimport %+v", st2)
	}
	list, err := d.ListLinks(b.ID, "", 0, 10)
	if err != nil || len(list) != 2 || list[0].URL != "https://example.com/b" {
		t.Fatalf("%+v %v", list, err)
	}
	hit, err := d.ListLinks(b.ID, "hello", 0, 10)
	if err != nil || len(hit) != 1 || !strings.Contains(hit[0].Title, "hello") {
		t.Fatalf("search %+v %v", hit, err)
	}
	if err := d.InsertLink(b.ID, "#ting", "bob", "https://example.com/c", "Title: c"); err != nil {
		t.Fatal(err)
	}
	n, _ := d.CountLinks(b.ID, "")
	if n != 3 {
		t.Fatalf("count %d", n)
	}
	e, ok, err := d.GetLink(b.ID, list[0].ID)
	if err != nil || !ok || e.URL != "https://example.com/b" {
		t.Fatalf("get %+v %v %v", e, ok, err)
	}
	st3, err := d.LinkStats(b.ID)
	if err != nil || st3.Total != 3 || st3.Domains != 1 {
		t.Fatalf("stats %+v %v", st3, err)
	}
	last, err := d.LastLinks(b.ID, 2)
	if err != nil || len(last) != 2 || last[0].URL != "https://example.com/c" {
		t.Fatalf("last %+v %v", last, err)
	}
	found, err := d.SearchLinks(b.ID, "hello")
	if err != nil || len(found) != 1 || found[0].Title != "Title: hello" {
		t.Fatalf("searchlinks %+v %v", found, err)
	}
	lines, next, more := FormatPage(last, 0)
	if len(lines) != 3 || next != 2 || more || !strings.Contains(lines[0], last[0].URL) {
		t.Fatalf("page lines=%v next=%d more=%v", lines, next, more)
	}
}

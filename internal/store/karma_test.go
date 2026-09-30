package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestKarmaAddGetImport(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}

	score, found, err := d.GetKarma(b.ID, "gone")
	if err != nil || found || score != 0 {
		t.Fatalf("missing: score=%d found=%v err=%v", score, found, err)
	}
	from, to, err := d.AddKarma(b.ID, "foo bar", 3)
	if err != nil || from != 0 || to != 3 {
		t.Fatalf("add: from=%d to=%d err=%v", from, to, err)
	}
	from, to, err = d.AddKarma(b.ID, "foo bar", -1)
	if err != nil || from != 3 || to != 2 {
		t.Fatalf("add2: from=%d to=%d err=%v", from, to, err)
	}

	src := filepath.Join(t.TempDir(), "karma-t3b-irc.example.net.db")
	sdb, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.Exec(`CREATE TABLE karma (phrase TEXT PRIMARY KEY NOT NULL, score INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.Exec(`INSERT INTO karma(phrase, score) VALUES ('t3b', 13), ('foo bar', 9)`); err != nil {
		t.Fatal(err)
	}
	sdb.Close()

	rows, err := ReadT3BKarma(src)
	if err != nil || len(rows) != 2 {
		t.Fatalf("read %+v %v", rows, err)
	}
	st, err := d.ImportKarma(b.ID, rows)
	if err != nil {
		t.Fatal(err)
	}
	if st.Imported != 1 || st.Duplicates != 1 || st.Total != 2 {
		t.Fatalf("import %+v", st)
	}
	score, found, err = d.GetKarma(b.ID, "foo bar")
	if err != nil || !found || score != 9 {
		t.Fatalf("replaced: score=%d found=%v", score, found)
	}
	st2, err := d.ImportKarma(b.ID, rows)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Imported != 0 || st2.Duplicates != 2 || st2.Total != 2 {
		t.Fatalf("reimport %+v", st2)
	}

	empty := filepath.Join(t.TempDir(), "empty.db")
	edb, err := sql.Open("sqlite", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edb.Exec(`CREATE TABLE nope (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	edb.Close()
	if _, err := ReadT3BKarma(empty); err == nil {
		t.Fatal("expected not a t3b karma db")
	}
	raw, err := os.ReadFile(src)
	if err != nil || !IsSQLite(raw) {
		t.Fatalf("magic %v", err)
	}
}

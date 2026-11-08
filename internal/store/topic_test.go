package store

import (
	"path/filepath"
	"testing"
)

func TestTopicLockAndHistory(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := d.ObserveTopic(b.ID, "#Ting", "hello"); err != nil {
		t.Fatal(err)
	}
	lock, ok, err := d.GetTopicLock(b.ID, "#ting")
	if err != nil || !ok || lock.Enabled || lock.Topic != "hello" || lock.Channel != "#ting" {
		t.Fatalf("observe %+v ok=%v %v", lock, ok, err)
	}

	if err := d.SetTopicLockEnabled(b.ID, "#TING", true); err != nil {
		t.Fatal(err)
	}
	if err := d.ObserveTopic(b.ID, "#ting", "drift"); err != nil {
		t.Fatal(err)
	}
	lock, _, err = d.GetTopicLock(b.ID, "#ting")
	if err != nil || !lock.Enabled || lock.Topic != "hello" {
		t.Fatalf("locked observe must not overwrite %+v %v", lock, err)
	}

	if err := d.SetTopicLockTopic(b.ID, "#ting", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendTopicHistory(b.ID, "#ting", "kept", "ann"); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendTopicHistory(b.ID, "#ting", "kept", "ann"); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendTopicHistory(b.ID, "#ting", "other", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := d.AppendTopicHistory(b.ID, "#ting", "", "bob"); err != nil {
		t.Fatal(err)
	}

	all, err := d.ListTopicHistory(b.ID, "#ting", "")
	if err != nil || len(all) != 2 || all[0].Topic != "other" || all[1].Topic != "kept" {
		t.Fatalf("list %+v %v", all, err)
	}
	hit, err := d.ListTopicHistory(b.ID, "#ting", "kep")
	if err != nil || len(hit) != 1 || hit[0].Topic != "kept" {
		t.Fatalf("search %+v %v", hit, err)
	}
}

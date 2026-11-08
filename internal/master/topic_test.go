package master

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamtew/ting/internal/control"
	"github.com/iamtew/ting/internal/store"
)

func TestChanOp(t *testing.T) {
	st := control.Status{Channels: map[string][]string{
		"#ting": {"@ann", "+bob", "carl", "~root", "%half"},
	}}
	if !chanOp(st, "#TING", "ann") || !chanOp(st, "#ting", "root") {
		t.Fatal("op/owner")
	}
	if chanOp(st, "#ting", "bob") || chanOp(st, "#ting", "carl") || chanOp(st, "#ting", "half") || chanOp(st, "#lab", "ann") {
		t.Fatal("not op")
	}
}

func TestTopicLockFlow(t *testing.T) {
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

	if restore, err := applyTopicSeen(d, b.ID, "332", "#ting", "hello"); err != nil || restore != "" {
		t.Fatalf("observe restore=%q %v", restore, err)
	}

	got := topicActs(d, p, b.ID, "#ting", "ann", nil)
	if len(got) != 1 || got[0].Text != topicUsage {
		t.Fatalf("usage %+v", got)
	}

	got = topicActs(d, p, b.ID, "#ting", "ann", []string{"on"})
	if len(got) != 2 || got[0].Text != "maintaining topic lock" || got[1].Kind != "topic" || got[1].Text != "hello" {
		t.Fatalf("enable %+v", got)
	}

	restore, err := applyTopicSeen(d, b.ID, "TOPIC", "#ting", "drift")
	if err != nil || restore != "hello" {
		t.Fatalf("restore %q %v", restore, err)
	}
	restore, err = applyTopicSeen(d, b.ID, "TOPIC", "#ting", "hello")
	if err != nil || restore != "" {
		t.Fatalf("match %q %v", restore, err)
	}

	got = topicActs(d, p, b.ID, "#ting", "ann", []string{"new", "topic"})
	if len(got) != 1 || got[0].Kind != "topic" || got[0].Text != "new topic" {
		t.Fatalf("set %+v", got)
	}

	got = topicActs(d, p, b.ID, "#ting", "ann", []string{"h"})
	if len(got) < 2 || !strings.Contains(got[0].Text, "new topic") || !strings.Contains(got[len(got)-1].Text, "Showing") {
		t.Fatalf("history %+v", got)
	}
	got = topicActs(d, p, b.ID, "#ting", "ann", []string{"h", "nope"})
	if len(got) != 1 || got[0].Text != "no matches" {
		t.Fatalf("search %+v", got)
	}

	got = topicActs(d, p, b.ID, "#ting", "ann", []string{"off"})
	if len(got) != 1 || got[0].Text != "no longer maintaining topic lock" {
		t.Fatalf("disable %+v", got)
	}
	restore, err = applyTopicSeen(d, b.ID, "TOPIC", "#ting", "later")
	if err != nil || restore != "" {
		t.Fatalf("unlocked %q %v", restore, err)
	}

	got = topicActs(d, p, b.ID, "#ting", "ann", []string{"lock"})
	if len(got) != 2 || got[1].Text != "later" {
		t.Fatalf("reenable %+v", got)
	}

	if Allowed("", "topic") || Allowed("", "t") {
		t.Fatal("ACL must not own public topic")
	}
}

func TestTopicEnableQueriesWhenUnknown(t *testing.T) {
	d, err := store.Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(store.Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	got := topicActs(d, newLinkPager(), b.ID, "#lab", "ann", []string{"enable"})
	if len(got) != 2 || got[1].Kind != "topicquery" {
		t.Fatalf("%+v", got)
	}
	restore, err := applyTopicSeen(d, b.ID, "TOPIC", "#lab", "spam")
	if err != nil || restore != "" {
		t.Fatalf("user topic must not fill empty lock %q %v", restore, err)
	}
	lock, ok, err := d.GetTopicLock(b.ID, "#lab")
	if err != nil || !ok || !lock.Enabled || lock.Topic != "" {
		t.Fatalf("empty lock %+v %v %v", lock, ok, err)
	}
	restore, err = applyTopicSeen(d, b.ID, "332", "#lab", "filled")
	if err != nil || restore != "" {
		t.Fatalf("fill %q %v", restore, err)
	}
	lock, ok, err = d.GetTopicLock(b.ID, "#lab")
	if err != nil || !ok || !lock.Enabled || lock.Topic != "filled" {
		t.Fatalf("filled %+v %v %v", lock, ok, err)
	}
	restore, err = applyTopicSeen(d, b.ID, "TOPIC", "#lab", "spam")
	if err != nil || restore != "filled" {
		t.Fatalf("restore after fill %q %v", restore, err)
	}
}

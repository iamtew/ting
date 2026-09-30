package master

import "testing"

func TestStripNick(t *testing.T) {
	if got := stripNick("ting", "ting: hello"); got != "hello" {
		t.Fatalf("strip %q", got)
	}
	if nickMentioned("ting", "hello there") {
		t.Fatal("bare words should not mention")
	}
	if nickMentioned("ting", "tingting") {
		t.Fatal("prefix of a longer word")
	}
	if !nickMentioned("Ting", "hey ting") {
		t.Fatal("case")
	}
}

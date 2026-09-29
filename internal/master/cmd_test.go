package master

import (
	"testing"

	"github.com/iamtew/ting/internal/control"
)

func TestParseDot(t *testing.T) {
	name, args, ok := ParseDot("  .say #ting hi there")
	if !ok || name != "say" || len(args) != 3 {
		t.Fatalf("%s %v %v", name, args, ok)
	}
	if _, _, ok := ParseDot("hello"); ok {
		t.Fatal("not a command")
	}
}

func TestSayDispatch(t *testing.T) {
	acts := Dispatch("admin", "#ting", "you", "say", []string{"#lab", "hello", "x"}, control.Status{})
	if len(acts) != 1 || acts[0].Kind != "privmsg" || acts[0].Target != "#lab" || acts[0].Text != "hello x" {
		t.Fatalf("%+v", acts)
	}
	acts = Dispatch("owner", "ting", "you", "stop", nil, control.Status{})
	if len(acts) != 1 || acts[0].Kind != "shutdown" {
		t.Fatalf("%+v", acts)
	}
	if Dispatch("admin", "#ting", "you", "stop", nil, control.Status{}) != nil {
		t.Fatal("admin must not stop")
	}
}

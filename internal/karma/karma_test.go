package karma

import (
	"strings"
	"testing"
)

func TestParseBumpPlain(t *testing.T) {
	b, ok := ParseBump("foobar++")
	if !ok || b.Phrase != "foobar" || b.Delta != 1 || b.Mode != ModeSilent || b.Reject != "" {
		t.Fatalf("foobar++: %+v ok=%v", b, ok)
	}
	b, ok = ParseBump("foo bar++")
	if !ok || b.Phrase != "foo bar" || b.Delta != 1 {
		t.Fatalf("foo bar++: %+v", b)
	}
	b, ok = ParseBump("foo bar-- baz")
	if !ok || b.Phrase != "foo bar" || b.Delta != -1 || b.Mode != ModeSilent {
		t.Fatalf("foo bar-- baz: %+v", b)
	}
	b, ok = ParseBump("food++")
	if !ok || b.Phrase != "food" || b.Delta != 1 || b.Mode != ModeSilent {
		t.Fatalf("food++ must be plain not dice: %+v", b)
	}
	if _, ok := ParseBump("++"); ok {
		t.Fatal("empty phrase")
	}
	if _, ok := ParseBump("hello world"); ok {
		t.Fatal("no operator")
	}
}

func TestParseBumpDice(t *testing.T) {
	b, ok := ParseBump("foobar+d")
	if !ok || b.Phrase != "foobar" || b.Mode != ModeDice || b.Reject != "" {
		t.Fatalf("+d: %+v ok=%v", b, ok)
	}
	if b.Result < 1 || b.Result > 6 || b.Delta != b.Result {
		t.Fatalf("+d roll: result=%d delta=%d", b.Result, b.Delta)
	}
	b, ok = ParseBump("foobar-d")
	if !ok || b.Mode != ModeDice || b.Delta != -b.Result {
		t.Fatalf("-d: %+v", b)
	}
	b, ok = ParseBump("x+d trailing")
	if !ok || b.Phrase != "x" || b.Mode != ModeDice {
		t.Fatalf("+d trailing: %+v", b)
	}
}

func TestParseBumpRandom(t *testing.T) {
	b, ok := ParseBump("foobar+1..6")
	if !ok || b.Phrase != "foobar" || b.Mode != ModeRandom || b.Reject != "" {
		t.Fatalf("+N..M: %+v ok=%v", b, ok)
	}
	if b.Result < 1 || b.Result > 6 || b.Delta != b.Result {
		t.Fatalf("+N..M roll: %+v", b)
	}
	b, ok = ParseBump("foo+2..6")
	if !ok || b.Phrase != "foo" || b.Mode != ModeRandom {
		t.Fatalf("foo+2..6: %+v", b)
	}
	b, ok = ParseBump("x-2..2")
	if !ok || b.Result != 2 || b.Delta != -2 {
		t.Fatalf("-2..2: %+v", b)
	}
	b, ok = ParseBump("nope+1..24")
	if !ok || b.Reject == "" || b.Delta != 0 {
		t.Fatalf("reject hi: %+v", b)
	}
	if !strings.Contains(b.Reject, "hakuna yer tata's") {
		t.Fatalf("reject msg: %q", b.Reject)
	}
	b, ok = ParseBump("nope+24..1")
	if !ok || b.Reject == "" {
		t.Fatalf("reject either bound: %+v", b)
	}
	b, ok = ParseBump("food++")
	if !ok || b.Mode != ModeSilent || b.Phrase != "food" {
		t.Fatalf("food++ vs range: %+v", b)
	}
}

func TestAdjustReply(t *testing.T) {
	if got := AdjustReply(ModeSilent, 1, 0, 1); got != "" {
		t.Fatalf("silent: %q", got)
	}
	got := AdjustReply(ModeDice, 4, 10, 14)
	want := "karma adjusted with dice roll 4, new karma: 10 -> 14"
	if got != want {
		t.Fatalf("dice: %q", got)
	}
	got = AdjustReply(ModeRandom, 7, 1, -6)
	want = "karma adjusted with random 7, new karma: 1 -> -6"
	if got != want {
		t.Fatalf("random: %q", got)
	}
}

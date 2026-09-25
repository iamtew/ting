package gateway

import (
	"testing"

	"github.com/iamtew/tng/internal/config"
)

func testCfg() config.Config {
	return config.Config{
		Identity: config.Identity{Nick: "tng", User: "tng", Realname: "tng"},
		Server:   config.Server{Host: "127.0.0.1", Port: 1},
	}
}

func TestParse(t *testing.T) {
	m := Parse(":nick!~u@host PRIVMSG #tng :hello there")
	if m.Nick != "nick" || m.User != "~u" || m.Host != "host" {
		t.Fatalf("prefix: %+v", m)
	}
	if m.Command != "PRIVMSG" || len(m.Params) != 2 || m.Params[1] != "hello there" {
		t.Fatalf("msg: %+v", m)
	}
	m = Parse("PING :irc.example.net")
	if m.Command != "PING" || m.Last() != "irc.example.net" {
		t.Fatalf("ping: %+v", m)
	}
	m = Parse(":a!b@c JOIN :#Chan")
	if m.Last() != "#Chan" {
		t.Fatalf("join: %+v", m)
	}
	if Parse(m.Encode()).Last() != "#Chan" {
		t.Fatalf("encode roundtrip %q", m.Encode())
	}
}

func TestSASLPlain(t *testing.T) {
	// PLAIN: NUL authcid NUL passwd
	got := saslPlain("bot", "s3cret")
	want := "AGJvdABzM2NyZXQ=" // echo -n '\0bot\0s3cret' | base64
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMaps(t *testing.T) {
	g := New(testCfg(), nil)
	_ = g.handle(Parse(":tng!~t@h JOIN :#tng"))
	_ = g.handle(Parse(":irc 353 tng = #tng :@tng +alice bob"))
	nicks := g.ChannelNicks("#TNG")
	if len(nicks) != 3 {
		t.Fatalf("nicks %v", nicks)
	}
	_ = g.handle(Parse(":bob!~b@h QUIT :bye"))
	if len(g.ChannelNicks("#tng")) != 2 {
		t.Fatalf("after quit %v", g.ChannelNicks("#tng"))
	}
	_ = g.handle(Parse(":alice!~a@h NICK :ally"))
	found := map[string]bool{}
	for _, n := range g.ChannelNicks("#tng") {
		found[n] = true
	}
	if !found["ally"] || found["alice"] {
		t.Fatalf("nick change %v", g.ChannelNicks("#tng"))
	}
}

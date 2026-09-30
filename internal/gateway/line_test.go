package gateway

import (
	"testing"

	"github.com/iamtew/ting/internal/config"
)

func testCfg() Spec {
	return Spec{
		Identity: config.Identity{Nick: "ting", User: "ting", Realname: "ting"},
		Server:   config.Server{Host: "127.0.0.1", Port: 1},
	}
}

func TestParse(t *testing.T) {
	m := Parse(":nick!~u@host PRIVMSG #ting :hello there")
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
	_ = g.handle(Parse(":ting!~t@h JOIN :#ting"))
	_ = g.handle(Parse(":irc 353 ting = #ting :@ting +alice bob"))
	nicks := g.ChannelNicks("#TING")
	if len(nicks) != 3 {
		t.Fatalf("nicks %v", nicks)
	}
	if nicks[0] != "@ting" || nicks[1] != "+alice" || nicks[2] != "bob" {
		t.Fatalf("order %v", nicks)
	}
	_ = g.handle(Parse(":bob!~b@h QUIT :bye"))
	if len(g.ChannelNicks("#ting")) != 2 {
		t.Fatalf("after quit %v", g.ChannelNicks("#ting"))
	}
	_ = g.handle(Parse(":alice!~a@h NICK :ally"))
	nicks = g.ChannelNicks("#ting")
	if len(nicks) != 2 || nicks[0] != "@ting" || nicks[1] != "+ally" {
		t.Fatalf("nick change %v", nicks)
	}
}

func TestModePrefix(t *testing.T) {
	g := New(testCfg(), nil)
	_ = g.handle(Parse(":ting!~t@h JOIN :#ting"))
	_ = g.handle(Parse(":irc 353 ting = #ting :ting alice"))
	_ = g.handle(Parse(":x!u@h MODE #ting +o alice"))
	nicks := g.ChannelNicks("#ting")
	if len(nicks) != 2 || nicks[0] != "@alice" || nicks[1] != "ting" {
		t.Fatalf("op %v", nicks)
	}
	_ = g.handle(Parse(":x!u@h MODE #ting -o+v alice alice"))
	nicks = g.ChannelNicks("#ting")
	if nicks[0] != "+alice" {
		t.Fatalf("voice %v", nicks)
	}
}

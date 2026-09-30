package store

import (
	"path/filepath"
	"testing"

	"github.com/iamtew/ting/internal/config"
)

func TestPutGetIdentityMergeImport(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	b, err := d.Put(Bundle{
		Host: "irc.example.net", TLS: true, Channels: []string{"#ting", " #ting "},
		Owners: []string{"you!*@*"}, Nick: "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "irc.example.net" || got.Port != 6697 || len(got.Channels) != 1 || got.Channels[0] != "#ting" {
		t.Fatalf("%+v", got)
	}
	id := MergeIdentity(config.Identity{Nick: "ting", User: "ting", Realname: "ting"}, got.Nick, got.User, got.Realname)
	if id.Nick != "bot" || id.User != "ting" {
		t.Fatalf("merge %+v", id)
	}

	d2, err := Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	c := config.Config{
		Server:   config.Server{Host: "legacy.net", Port: 6697, TLS: true},
		Channels: []string{"#a"},
		Owners:   []string{"o!*@*"},
	}
	if err := d2.ImportLegacy(c); err != nil {
		t.Fatal(err)
	}
	if err := d2.ImportLegacy(c); err != nil {
		t.Fatal(err)
	}
	n, _ := d2.Count()
	if n != 1 {
		t.Fatalf("count %d", n)
	}
}

func TestValidateSASLNeedsTLS(t *testing.T) {
	b := Bundle{Host: "irc.example.net", Port: 6667, TLS: false, SASLEnabled: true, SASLMechanism: "PLAIN", SASLUser: "ting", SASLPassword: "x"}
	if err := b.Validate(); err == nil {
		t.Fatal("expected SASL without TLS to fail")
	}
	b.TLS = true
	b.Port = 6697
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSpecEqualIgnoresNameACL(t *testing.T) {
	g := config.Identity{Nick: "ting", User: "ting", Realname: "ting"}
	a := Bundle{Host: "irc.example.net", Port: 6697, TLS: true, Name: "a", Owners: []string{"x!*@*"}}
	b := Bundle{Host: "irc.example.net", Port: 6697, TLS: true, Name: "b", Admins: []string{"y!*@*"}}
	if !SpecEqual(g, a, b) {
		t.Fatal("name/acl must not force restart")
	}
	b.Host = "other.net"
	if SpecEqual(g, a, b) {
		t.Fatal("host change must not be equal")
	}
	b.Host = a.Host
	b.Channels = []string{"#x"}
	if SpecEqual(g, a, b) {
		t.Fatal("channel change must not be equal")
	}
	if !DialEqual(g, a, b) {
		t.Fatal("channel-only must still dial-equal")
	}
	b.Channels = nil
	off := false
	b.Twitter = &off
	if !SpecEqual(g, a, b) {
		t.Fatal("resolve flags must not force restart")
	}
	b.Host = "other.net"
	if DialEqual(g, a, b) {
		t.Fatal("host change must not be dial-equal")
	}
}

func TestConnectorPersist(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, err := d.Put(Bundle{Host: "irc.example.net", TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.PutConnector(Connector{ServerID: b.ID, Listen: "127.0.0.1:1", Token: "t", PID: 7}); err != nil {
		t.Fatal(err)
	}
	c, err := d.Connector(b.ID)
	if err != nil || c.Listen != "127.0.0.1:1" || c.Token != "t" || c.PID != 7 {
		t.Fatalf("%+v %v", c, err)
	}
	if err := d.ClearConnector(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Connector(b.ID); err == nil {
		t.Fatal("want gone")
	}
}

func TestResolveFlagsRoundTrip(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	off := false
	b, err := d.Put(Bundle{Host: "irc.example.net", TLS: true, Twitter: &off, YouTubeAPIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Twitter == nil || *got.Twitter {
		t.Fatalf("twitter want off %+v", got.Twitter)
	}
	if got.URLTitles == nil || !*got.URLTitles {
		t.Fatalf("url_titles want on %+v", got.URLTitles)
	}
	if got.YouTubeAPIKey != "k" {
		t.Fatalf("key %q", got.YouTubeAPIKey)
	}
	if got.HTTPTimeoutSec != 8 {
		t.Fatalf("timeout %d", got.HTTPTimeoutSec)
	}
}

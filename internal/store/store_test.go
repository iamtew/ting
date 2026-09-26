package store

import (
	"path/filepath"
	"testing"

	"github.com/iamtew/tng/internal/config"
)

func TestPutGetIdentityMergeImport(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "tng.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	b, err := d.Put(Bundle{
		Host: "irc.example.net", TLS: true, Channels: []string{"#tng", " #tng "},
		Owners: []string{"you!*@*"}, Nick: "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "irc.example.net" || got.Port != 6697 || len(got.Channels) != 1 || got.Channels[0] != "#tng" {
		t.Fatalf("%+v", got)
	}
	id := MergeIdentity(config.Identity{Nick: "tng", User: "tng", Realname: "tng"}, got.Nick, got.User, got.Realname)
	if id.Nick != "bot" || id.User != "tng" {
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
	b := Bundle{Host: "irc.example.net", Port: 6667, TLS: false, SASLEnabled: true, SASLMechanism: "PLAIN", SASLUser: "tng", SASLPassword: "x"}
	if err := b.Validate(); err == nil {
		t.Fatal("expected SASL without TLS to fail")
	}
	b.TLS = true
	b.Port = 6697
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
}

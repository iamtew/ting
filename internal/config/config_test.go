package config

import "testing"

func TestValidateSASLNeedsTLS(t *testing.T) {
	c := Config{
		Server:   Server{Host: "irc.example.net", Port: 6667, TLS: false},
		Identity: Identity{Nick: "tng"},
		SASL:     SASL{Enabled: true, Mechanism: "PLAIN", User: "tng", Password: "x"},
	}
	c.applyDefaults()
	if err := c.Validate(); err == nil {
		t.Fatal("expected SASL without TLS to fail")
	}
	c.Server.TLS = true
	c.Server.Port = 6697
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

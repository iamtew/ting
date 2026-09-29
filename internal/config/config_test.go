package config

import "testing"

func TestValidateNoServer(t *testing.T) {
	c := Config{
		Identity: Identity{Nick: "ting"},
		Control:  Control{Token: "x"},
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Database != DefaultDatabase {
		t.Fatalf("db %q", c.Database)
	}
}

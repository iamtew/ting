package acl

import "testing"

func TestMatch(t *testing.T) {
	if !Match("you!ident@host", "you!ident@host") {
		t.Fatal("exact")
	}
	if !Match("*!*@host", "you!ident@host") {
		t.Fatal("star")
	}
	if Match("other!*@*", "you!ident@host") {
		t.Fatal("other")
	}
	if Role([]string{"you!*@*"}, nil, "you", "ident", "host") != "owner" {
		t.Fatal("owner")
	}
	if Role(nil, []string{"*!*@host"}, "a", "b", "host") != "admin" {
		t.Fatal("admin")
	}
	if Role(nil, nil, "a", "b", "c") != "" {
		t.Fatal("none")
	}
}

package acl

import "strings"

func Role(owners, admins []string, nick, user, host string) string {
	mask := nick + "!" + user + "@" + host
	for _, p := range owners {
		if Match(p, mask) {
			return "owner"
		}
	}
	for _, p := range admins {
		if Match(p, mask) {
			return "admin"
		}
	}
	return ""
}

func Match(pattern, mask string) bool {
	return glob(strings.ToLower(pattern), strings.ToLower(mask))
}

func glob(p, s string) bool {
	for {
		if p == "" {
			return s == ""
		}
		if p[0] == '*' {
			p = p[1:]
			if p == "" {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if glob(p, s[i:]) {
					return true
				}
			}
			return false
		}
		if s == "" {
			return false
		}
		if p[0] != '?' && p[0] != s[0] {
			return false
		}
		p, s = p[1:], s[1:]
	}
}

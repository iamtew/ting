package gateway

import (
	"strings"
	"unicode"
)

// Message is one IRC line. Params[len-1] is trailing when the source used ':'.
type Message struct {
	Prefix  string
	Nick    string
	User    string
	Host    string
	Command string
	Params  []string
}

func Parse(line string) Message {
	line = strings.TrimRight(line, "\r\n")
	var m Message
	if line == "" {
		return m
	}
	rest := line
	if rest[0] == ':' {
		var prefix string
		prefix, rest = splitOnce(rest[1:], ' ')
		m.Prefix = prefix
		m.Nick, m.User, m.Host = splitPrefix(prefix)
	}
	if rest == "" {
		return m
	}
	cmd, rest := splitOnce(rest, ' ')
	m.Command = cmd
	for rest != "" {
		if rest[0] == ':' {
			m.Params = append(m.Params, rest[1:])
			break
		}
		var p string
		p, rest = splitOnce(rest, ' ')
		if p != "" {
			m.Params = append(m.Params, p)
		}
	}
	return m
}

func (m Message) Encode() string {
	var b strings.Builder
	if m.Prefix != "" {
		b.WriteByte(':')
		b.WriteString(m.Prefix)
		b.WriteByte(' ')
	}
	b.WriteString(m.Command)
	for i, p := range m.Params {
		b.WriteByte(' ')
		if i == len(m.Params)-1 && (p == "" || strings.ContainsAny(p, " \t") || strings.HasPrefix(p, ":")) {
			b.WriteByte(':')
		}
		b.WriteString(p)
	}
	return b.String()
}

func (m Message) Last() string {
	if len(m.Params) == 0 {
		return ""
	}
	return m.Params[len(m.Params)-1]
}

func splitOnce(s string, sep byte) (string, string) {
	i := strings.IndexByte(s, sep)
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeftFunc(s[i+1:], unicode.IsSpace)
}

func splitPrefix(p string) (nick, user, host string) {
	bang := strings.IndexByte(p, '!')
	at := strings.IndexByte(p, '@')
	if bang < 0 || at < 0 || at < bang {
		return p, "", ""
	}
	return p[:bang], p[bang+1 : at], p[at+1:]
}

func fold(s string) string { return strings.ToLower(s) }

func stripStatus(nick string) string {
	for len(nick) > 0 {
		switch nick[0] {
		case '@', '+', '%', '~', '&':
			nick = nick[1:]
		default:
			return nick
		}
	}
	return nick
}

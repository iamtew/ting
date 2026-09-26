package master

import (
	"strings"

	"github.com/iamtew/tng/internal/control"
)

type Act struct {
	Kind   string // privmsg, join, part, mode, nick, shutdown
	Target string
	Text   string
}

func ParseDot(text string) (name string, args []string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, ".") {
		return "", nil, false
	}
	f := strings.Fields(text[1:])
	if len(f) == 0 {
		return "", nil, false
	}
	return strings.ToLower(f[0]), f[1:], true
}

func Allowed(role, name string) bool {
	switch name {
	case "join", "leave", "op", "deop", "help", "status", "say":
		return role == "owner" || role == "admin"
	case "stop", "nick":
		return role == "owner"
	default:
		return false
	}
}

func Dispatch(role, dest, sender, name string, args []string, st control.Status) []Act {
	if !Allowed(role, name) {
		return nil
	}
	reply := dest
	if !isChan(dest) {
		reply = sender
	}
	switch name {
	case "help":
		return []Act{{Kind: "privmsg", Target: reply, Text: helpText(role)}}
	case "status":
		return []Act{{Kind: "privmsg", Target: reply, Text: statusText(st)}}
	case "say":
		target, text := sayTarget(dest, sender, args)
		if target == "" {
			return []Act{{Kind: "privmsg", Target: reply, Text: "usage: .say [#chan] text"}}
		}
		return []Act{{Kind: "privmsg", Target: target, Text: text}}
	case "join":
		if len(args) < 1 {
			return []Act{{Kind: "privmsg", Target: reply, Text: "usage: .join #chan"}}
		}
		return []Act{{Kind: "join", Target: args[0]}}
	case "leave":
		ch := ""
		if len(args) > 0 {
			ch = args[0]
		} else if isChan(dest) {
			ch = dest
		}
		if ch == "" {
			return []Act{{Kind: "privmsg", Target: reply, Text: "usage: .leave #chan"}}
		}
		return []Act{{Kind: "part", Target: ch}}
	case "op", "deop":
		ch, nick := modeArgs(dest, args)
		if ch == "" || nick == "" {
			return []Act{{Kind: "privmsg", Target: reply, Text: "usage: ." + name + " [#chan] nick"}}
		}
		pm := "+o"
		if name == "deop" {
			pm = "-o"
		}
		return []Act{{Kind: "mode", Target: ch, Text: pm + " " + nick}}
	case "nick":
		if len(args) < 1 {
			return []Act{{Kind: "privmsg", Target: reply, Text: "usage: .nick newnick"}}
		}
		return []Act{{Kind: "nick", Text: args[0]}}
	case "stop":
		return []Act{{Kind: "shutdown"}}
	}
	return nil
}

func helpText(role string) string {
	s := ".help .status .say .join .leave .op .deop"
	if role == "owner" {
		s += " .nick .stop"
	}
	return s
}

func statusText(st control.Status) string {
	chs := make([]string, 0, len(st.Channels))
	for ch := range st.Channels {
		chs = append(chs, ch)
	}
	conn := "down"
	if st.Connected {
		conn = "up"
	}
	return conn + " " + st.Nick + " " + strings.Join(chs, " ")
}

func sayTarget(dest, sender string, args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	if isChan(args[0]) && len(args) >= 2 {
		return args[0], strings.Join(args[1:], " ")
	}
	text := strings.Join(args, " ")
	if isChan(dest) {
		return dest, text
	}
	return sender, text
}

func modeArgs(dest string, args []string) (ch, nick string) {
	if len(args) == 0 {
		return "", ""
	}
	if isChan(args[0]) {
		if len(args) < 2 {
			return "", ""
		}
		return args[0], args[1]
	}
	if isChan(dest) {
		return dest, args[0]
	}
	return "", ""
}

func isChan(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "&")
}

package master

import (
	"strings"

	"github.com/iamtew/ting/internal/control"
	"github.com/iamtew/ting/internal/gateway"
	"github.com/iamtew/ting/internal/store"
)

const topicUsage = "usage: .topic on|off | .topic status | .topic <text> | .topic history [q]  (.t)"

const statusMarks = "~&@%+"

func stripStatus(nick string) (pref, bare string) {
	i := 0
	for i < len(nick) && strings.IndexByte(statusMarks, nick[i]) >= 0 {
		i++
	}
	return nick[:i], nick[i:]
}

func chanOp(st control.Status, channel, nick string) bool {
	var list []string
	for k, v := range st.Channels {
		if strings.EqualFold(k, channel) {
			list = v
			break
		}
	}
	for _, n := range list {
		pref, bare := stripStatus(n)
		if strings.EqualFold(bare, nick) && strings.ContainsAny(pref, "~&@") {
			return true
		}
	}
	return false
}

func sendTopic(cl *control.Client, channel, text string) error {
	return cl.Raw(gateway.Message{Command: "TOPIC", Params: []string{channel, text}}.Encode())
}

// applyTopicSeen records unlocked topics and returns a restore string when locked and drifted.
// Only 332/331 may fill an empty lock (reply to TOPIC query). A user TOPIC is never adopted.
func applyTopicSeen(d *store.DB, serverID int64, cmd, channel, seen string) (restore string, err error) {
	if d == nil {
		return "", nil
	}
	lock, ok, err := d.GetTopicLock(serverID, channel)
	if err != nil {
		return "", err
	}
	if !ok || !lock.Enabled {
		return "", d.ObserveTopic(serverID, channel, seen)
	}
	cmd = strings.ToUpper(cmd)
	if lock.Topic == "" {
		if cmd != "332" && cmd != "331" {
			return "", nil
		}
		if seen == "" {
			return "", nil
		}
		if err := d.SetTopicLockTopic(serverID, channel, seen); err != nil {
			return "", err
		}
		return "", d.AppendTopicHistory(serverID, channel, seen, "")
	}
	if seen != lock.Topic {
		return lock.Topic, nil
	}
	return "", nil
}

func topicActs(d *store.DB, p *linkPager, serverID int64, dest, nick string, args []string) []Act {
	if d == nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic log unavailable"}}
	}
	if len(args) == 0 {
		return []Act{{Kind: "privmsg", Target: dest, Text: topicUsage}}
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "on", "enable", "lock":
		return topicEnable(d, serverID, dest, nick)
	case "off", "disable", "unlock":
		return topicDisable(d, serverID, dest)
	case "history", "h":
		q := strings.Join(args[1:], " ")
		return topicHistory(d, p, serverID, dest, nick, q)
	case "status", "state":
		return topicStatus(d, serverID, dest)
	}
	text := strings.Join(args, " ")
	if err := d.SetTopicLockTopic(serverID, dest, text); err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	if err := d.AppendTopicHistory(serverID, dest, text, nick); err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	return []Act{{Kind: "topic", Target: dest, Text: text}}
}

func topicEnable(d *store.DB, serverID int64, dest, nick string) []Act {
	lock, _, err := d.GetTopicLock(serverID, dest)
	if err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	if lock.Enabled {
		return []Act{{Kind: "privmsg", Target: dest, Text: "maintaining topic lock"}}
	}
	if err := d.SetTopicLockEnabled(serverID, dest, true); err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	lock, _, err = d.GetTopicLock(serverID, dest)
	if err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	if lock.Topic != "" {
		if err := d.AppendTopicHistory(serverID, dest, lock.Topic, nick); err != nil {
			return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
		}
		return []Act{
			{Kind: "privmsg", Target: dest, Text: "maintaining topic lock"},
			{Kind: "topic", Target: dest, Text: lock.Topic},
		}
	}
	return []Act{
		{Kind: "privmsg", Target: dest, Text: "maintaining topic lock"},
		{Kind: "topicquery", Target: dest},
	}
}

func topicStatus(d *store.DB, serverID int64, dest string) []Act {
	lock, _, err := d.GetTopicLock(serverID, dest)
	if err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	if !lock.Enabled {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic lock is off"}}
	}
	if lock.Topic == "" {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic lock is on"}}
	}
	return []Act{{Kind: "privmsg", Target: dest, Text: "topic lock is on, enforcing topic: " + lock.Topic}}
}

func topicDisable(d *store.DB, serverID int64, dest string) []Act {
	lock, _, err := d.GetTopicLock(serverID, dest)
	if err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	if !lock.Enabled {
		return []Act{{Kind: "privmsg", Target: dest, Text: "no longer maintaining topic lock"}}
	}
	if err := d.SetTopicLockEnabled(serverID, dest, false); err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic failed: " + err.Error()}}
	}
	return []Act{{Kind: "privmsg", Target: dest, Text: "no longer maintaining topic lock"}}
}

func topicHistory(d *store.DB, p *linkPager, serverID int64, dest, nick, q string) []Act {
	hits, err := d.ListTopicHistory(serverID, dest, q)
	if err != nil {
		return []Act{{Kind: "privmsg", Target: dest, Text: "topic history failed: " + err.Error()}}
	}
	if len(hits) == 0 {
		if strings.TrimSpace(q) != "" {
			return []Act{{Kind: "privmsg", Target: dest, Text: "no matches"}}
		}
		return []Act{{Kind: "privmsg", Target: dest, Text: "no topics in archive"}}
	}
	lines := make([]string, len(hits))
	for i, e := range hits {
		lines[i] = store.FormatTopicEntry(e)
	}
	var out []Act
	for _, line := range startPage(p, pagerKey(serverID, dest, nick), lines) {
		out = append(out, Act{Kind: "privmsg", Target: dest, Text: line})
	}
	return out
}

package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TopicLock is the per-channel lock row. Channel is folded.
type TopicLock struct {
	ServerID int64
	Channel  string
	Enabled  bool
	Topic    string
}

// TopicHist is one archived topic.
type TopicHist struct {
	ID       int64
	ServerID int64
	Channel  string
	Topic    string
	Nick     string
	Datetime string
}

func foldChan(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (d *DB) GetTopicLock(serverID int64, channel string) (TopicLock, bool, error) {
	var e TopicLock
	var en int
	err := d.sql.QueryRow(`
SELECT server_id, channel, enabled, topic FROM topic_lock WHERE server_id=? AND channel=?`,
		serverID, foldChan(channel)).Scan(&e.ServerID, &e.Channel, &en, &e.Topic)
	if err == sql.ErrNoRows {
		return e, false, nil
	}
	if err != nil {
		return e, false, err
	}
	e.Enabled = en != 0
	return e, true, nil
}

func (d *DB) putTopicLock(serverID int64, channel string, enabled bool, topic string) error {
	if serverID == 0 {
		return fmt.Errorf("server_id required")
	}
	channel = foldChan(channel)
	if channel == "" {
		return fmt.Errorf("channel required")
	}
	en := 0
	if enabled {
		en = 1
	}
	_, err := d.sql.Exec(`
INSERT INTO topic_lock (server_id, channel, enabled, topic) VALUES (?,?,?,?)
ON CONFLICT(server_id, channel) DO UPDATE SET enabled=excluded.enabled, topic=excluded.topic`,
		serverID, channel, en, topic)
	return err
}

// ObserveTopic stores last-seen topic while unlocked. No-op when lock is on.
func (d *DB) ObserveTopic(serverID int64, channel, topic string) error {
	lock, ok, err := d.GetTopicLock(serverID, channel)
	if err != nil {
		return err
	}
	if ok && lock.Enabled {
		return nil
	}
	return d.putTopicLock(serverID, channel, false, topic)
}

// SetTopicLockEnabled flips the lock flag and keeps the stored topic.
func (d *DB) SetTopicLockEnabled(serverID int64, channel string, enabled bool) error {
	lock, ok, err := d.GetTopicLock(serverID, channel)
	if err != nil {
		return err
	}
	topic := ""
	if ok {
		topic = lock.Topic
	}
	return d.putTopicLock(serverID, channel, enabled, topic)
}

// SetTopicLockTopic writes the lock target (and last-seen when creating a row).
func (d *DB) SetTopicLockTopic(serverID int64, channel, topic string) error {
	lock, ok, err := d.GetTopicLock(serverID, channel)
	if err != nil {
		return err
	}
	enabled := false
	if ok {
		enabled = lock.Enabled
	}
	return d.putTopicLock(serverID, channel, enabled, topic)
}

// AppendTopicHistory skips empty text and an exact duplicate of the newest row.
func (d *DB) AppendTopicHistory(serverID int64, channel, topic, nick string) error {
	if serverID == 0 {
		return fmt.Errorf("server_id required")
	}
	channel = foldChan(channel)
	topic = strings.TrimRight(topic, "\r\n")
	if channel == "" || topic == "" {
		return nil
	}
	last, ok, err := d.lastTopicHistory(serverID, channel)
	if err != nil {
		return err
	}
	if ok && last.Topic == topic {
		return nil
	}
	_, err = d.sql.Exec(`
INSERT INTO topic_history (server_id, channel, topic, nick, datetime) VALUES (?,?,?,?,?)`,
		serverID, channel, topic, strings.TrimSpace(nick), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (d *DB) lastTopicHistory(serverID int64, channel string) (TopicHist, bool, error) {
	var e TopicHist
	err := d.sql.QueryRow(`
SELECT id, server_id, channel, topic, nick, datetime
FROM topic_history WHERE server_id=? AND channel=? ORDER BY id DESC LIMIT 1`,
		serverID, channel).Scan(&e.ID, &e.ServerID, &e.Channel, &e.Topic, &e.Nick, &e.Datetime)
	if err == sql.ErrNoRows {
		return e, false, nil
	}
	if err != nil {
		return e, false, err
	}
	return e, true, nil
}

// ListTopicHistory is newest first. q filters topic text when non-empty.
func (d *DB) ListTopicHistory(serverID int64, channel, q string) ([]TopicHist, error) {
	channel = foldChan(channel)
	q = strings.TrimSpace(q)
	var rows *sql.Rows
	var err error
	if q == "" {
		rows, err = d.sql.Query(`
SELECT id, server_id, channel, topic, nick, datetime
FROM topic_history WHERE server_id=? AND channel=? ORDER BY id DESC`, serverID, channel)
	} else {
		like := likeArg(q)
		rows, err = d.sql.Query(`
SELECT id, server_id, channel, topic, nick, datetime
FROM topic_history WHERE server_id=? AND channel=? AND topic LIKE ? ESCAPE '\'
ORDER BY id DESC`, serverID, channel, like)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopicHist
	for rows.Next() {
		var e TopicHist
		if err := rows.Scan(&e.ID, &e.ServerID, &e.Channel, &e.Topic, &e.Nick, &e.Datetime); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// FormatTopicEntry is one IRC-friendly history line.
func FormatTopicEntry(e TopicHist) string {
	return fmt.Sprintf("#%d [%s] <%s> %s — %s", e.ID, e.Channel, e.Nick, e.Datetime, e.Topic)
}

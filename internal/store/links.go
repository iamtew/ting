package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// Link is one t3b-style resolved URL (JSONL field URL is uppercase).
type Link struct {
	ID       int64  `json:"id"`
	ServerID int64  `json:"server_id,omitempty"`
	Datetime string `json:"datetime"`
	Channel  string `json:"channel"`
	User     string `json:"user"`
	Domain   string `json:"domain"`
	URL      string `json:"URL"`
	Title    string `json:"title"`
}

// ImportStats is what Meat Bags see after a drop.
type ImportStats struct {
	Imported   int `json:"imported"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
	Total      int `json:"total"`
}

// ParseT3BLinks reads t3b links-*.log JSONL. Bad/empty lines count as skipped, not fatal.
func ParseT3BLinks(r io.Reader) (out []Link, skipped int, err error) {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Link
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			skipped++
			continue
		}
		e.URL = strings.TrimSpace(e.URL)
		e.Title = strings.TrimSpace(e.Title)
		e.Channel = strings.TrimSpace(e.Channel)
		e.User = strings.TrimSpace(e.User)
		e.Datetime = strings.TrimSpace(e.Datetime)
		if e.URL == "" || e.Title == "" {
			skipped++
			continue
		}
		if e.Domain == "" {
			e.Domain = domainOf(e.URL)
		}
		if e.Datetime == "" {
			e.Datetime = time.Now().UTC().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	return out, skipped, sc.Err()
}

func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (d *DB) InsertLink(serverID int64, channel, user, rawURL, title string) error {
	if serverID == 0 {
		return fmt.Errorf("server_id required")
	}
	rawURL = strings.TrimSpace(rawURL)
	title = strings.TrimSpace(title)
	if rawURL == "" || title == "" {
		return nil
	}
	_, err := d.sql.Exec(`
INSERT OR IGNORE INTO links (server_id, datetime, channel, user, domain, url, title)
VALUES (?,?,?,?,?,?,?)`,
		serverID, time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(channel), strings.TrimSpace(user),
		domainOf(rawURL), rawURL, title)
	return err
}

func (d *DB) ImportLinks(serverID int64, links []Link) (ImportStats, error) {
	var st ImportStats
	if serverID == 0 {
		return st, fmt.Errorf("server_id required")
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return st, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
INSERT OR IGNORE INTO links (server_id, datetime, channel, user, domain, url, title)
VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return st, err
	}
	defer stmt.Close()
	for _, e := range links {
		res, err := stmt.Exec(serverID, e.Datetime, e.Channel, e.User, e.Domain, e.URL, e.Title)
		if err != nil {
			return st, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			st.Duplicates++
		} else {
			st.Imported++
		}
	}
	if err := tx.Commit(); err != nil {
		return st, err
	}
	st.Total, err = d.CountLinks(serverID, "")
	return st, err
}

func likeArg(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return "%" + q + "%"
}

func linkFilter(serverID int64, q string) (where string, args []any) {
	where = `server_id=?`
	args = []any{serverID}
	q = strings.TrimSpace(q)
	if q == "" {
		return where, args
	}
	like := likeArg(q)
	where += ` AND (url LIKE ? ESCAPE '\' OR title LIKE ? ESCAPE '\' OR domain LIKE ? ESCAPE '\' OR channel LIKE ? ESCAPE '\' OR user LIKE ? ESCAPE '\')`
	args = append(args, like, like, like, like, like)
	return where, args
}

func (d *DB) CountLinks(serverID int64, q string) (int, error) {
	w, args := linkFilter(serverID, q)
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM links WHERE `+w, args...).Scan(&n)
	return n, err
}

func (d *DB) ListLinks(serverID int64, q string, offset, limit int) ([]Link, error) {
	if limit <= 0 || limit > 200 {
		limit = 80
	}
	if offset < 0 {
		offset = 0
	}
	w, args := linkFilter(serverID, q)
	args = append(args, limit, offset)
	rows, err := d.sql.Query(`
SELECT id, server_id, datetime, channel, user, domain, url, title
FROM links WHERE `+w+` ORDER BY datetime DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var e Link
		if err := rows.Scan(&e.ID, &e.ServerID, &e.Datetime, &e.Channel, &e.User, &e.Domain, &e.URL, &e.Title); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

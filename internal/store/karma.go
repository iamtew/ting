package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// sqliteMagic is the first 16 bytes of a SQLite 3 file.
var sqliteMagic = []byte("SQLite format 3\x00")

// KarmaRow is one phrase score from a t3b karma-*.db.
type KarmaRow struct {
	Phrase string
	Score  int
}

// IsSQLite reports a SQLite 3 header.
func IsSQLite(b []byte) bool {
	return len(b) >= len(sqliteMagic) && bytes.Equal(b[:len(sqliteMagic)], sqliteMagic)
}

func (d *DB) GetKarma(serverID int64, phrase string) (score int, found bool, err error) {
	err = d.sql.QueryRow(`SELECT score FROM karma WHERE server_id=? AND phrase=?`, serverID, phrase).Scan(&score)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return score, true, nil
}

func (d *DB) AddKarma(serverID int64, phrase string, delta int) (from, to int, err error) {
	if serverID == 0 {
		return 0, 0, fmt.Errorf("server_id required")
	}
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return 0, 0, fmt.Errorf("phrase required")
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRow(`SELECT score FROM karma WHERE server_id=? AND phrase=?`, serverID, phrase).Scan(&from)
	if err == sql.ErrNoRows {
		from = 0
		to = delta
		_, err = tx.Exec(`INSERT INTO karma(server_id, phrase, score) VALUES(?,?,?)`, serverID, phrase, to)
	} else if err != nil {
		return 0, 0, err
	} else {
		to = from + delta
		_, err = tx.Exec(`UPDATE karma SET score=? WHERE server_id=? AND phrase=?`, to, serverID, phrase)
	}
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return from, to, nil
}

func (d *DB) CountKarma(serverID int64) (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM karma WHERE server_id=?`, serverID).Scan(&n)
	return n, err
}

func (d *DB) ImportKarma(serverID int64, rows []KarmaRow) (ImportStats, error) {
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
INSERT INTO karma(server_id, phrase, score) VALUES(?,?,?)
ON CONFLICT(server_id, phrase) DO UPDATE SET score=excluded.score`)
	if err != nil {
		return st, err
	}
	defer stmt.Close()
	for _, row := range rows {
		phrase := strings.TrimSpace(row.Phrase)
		if phrase == "" {
			st.Skipped++
			continue
		}
		var dummy int
		err := tx.QueryRow(`SELECT score FROM karma WHERE server_id=? AND phrase=?`, serverID, phrase).Scan(&dummy)
		found := err == nil
		if err != nil && err != sql.ErrNoRows {
			return st, err
		}
		if _, err := stmt.Exec(serverID, phrase, row.Score); err != nil {
			return st, err
		}
		if found {
			st.Duplicates++
		} else {
			st.Imported++
		}
	}
	if err := tx.Commit(); err != nil {
		return st, err
	}
	st.Total, err = d.CountKarma(serverID)
	return st, err
}

// ReadT3BKarma opens a t3b karma-*.db and reads phrase, score.
func ReadT3BKarma(path string) ([]KarmaRow, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT phrase, score FROM karma`)
	if err != nil {
		return nil, fmt.Errorf("not a t3b karma db")
	}
	defer rows.Close()
	var out []KarmaRow
	for rows.Next() {
		var r KarmaRow
		if err := rows.Scan(&r.Phrase, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WriteTempSQLite dumps bytes to a temp file for ReadT3BKarma. Caller must Remove.
func WriteTempSQLite(b []byte) (string, error) {
	f, err := os.CreateTemp("", "ting-karma-*.db")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

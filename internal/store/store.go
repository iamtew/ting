package store

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/iamtew/ting/internal/config"
	"github.com/iamtew/ting/internal/gateway"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

type Connector struct {
	ServerID int64
	Listen   string
	Token    string
	PID      int
}

type Bundle struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	TLS              bool     `json:"tls"`
	TLSSkipVerify    bool     `json:"tls_skip_verify"`
	Enabled          bool     `json:"enabled"`
	Nick             string   `json:"nick"`
	User             string   `json:"user"`
	Realname         string   `json:"realname"`
	NickServPassword string   `json:"nickserv_password"`
	SASLEnabled      bool     `json:"sasl_enabled"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	SASLUser         string   `json:"sasl_user"`
	SASLPassword     string   `json:"sasl_password"`
	Channels         []string `json:"channels"`
	Owners           []string `json:"owners"`
	Admins           []string `json:"admins"`
}

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		sqlDB.Close()
		return nil, err
	}
	d := &DB{sql: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) migrate() error {
	_, err := d.sql.Exec(`
CREATE TABLE IF NOT EXISTS servers (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  host TEXT NOT NULL,
  port INTEGER NOT NULL,
  tls INTEGER NOT NULL DEFAULT 1,
  tls_skip_verify INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  nick TEXT NOT NULL DEFAULT '',
  user TEXT NOT NULL DEFAULT '',
  realname TEXT NOT NULL DEFAULT '',
  nickserv_password TEXT NOT NULL DEFAULT '',
  sasl_enabled INTEGER NOT NULL DEFAULT 0,
  sasl_mechanism TEXT NOT NULL DEFAULT 'PLAIN',
  sasl_user TEXT NOT NULL DEFAULT '',
  sasl_password TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS channels (
  server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  PRIMARY KEY (server_id, name)
);
CREATE TABLE IF NOT EXISTS acl (
  server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK(role IN ('owner','admin')),
  mask TEXT NOT NULL,
  PRIMARY KEY (server_id, role, mask)
);
CREATE TABLE IF NOT EXISTS connector (
  server_id INTEGER PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
  listen TEXT NOT NULL,
  token TEXT NOT NULL,
  pid INTEGER NOT NULL DEFAULT 0
);`)
	return err
}

func MergeIdentity(g config.Identity, nick, user, realname string) config.Identity {
	out := g
	if s := strings.TrimSpace(nick); s != "" {
		out.Nick = s
	}
	if s := strings.TrimSpace(user); s != "" {
		out.User = s
	}
	if s := strings.TrimSpace(realname); s != "" {
		out.Realname = s
	}
	if out.User == "" {
		out.User = out.Nick
	}
	if out.Realname == "" {
		out.Realname = out.Nick
	}
	return out
}

func SpecEqual(g config.Identity, a, b Bundle) bool {
	return reflect.DeepEqual(Spec(g, a), Spec(g, b))
}

func DialEqual(g config.Identity, a, b Bundle) bool {
	sa, sb := Spec(g, a), Spec(g, b)
	sa.Channels, sb.Channels = nil, nil
	return reflect.DeepEqual(sa, sb)
}

func Spec(g config.Identity, b Bundle) gateway.Spec {
	id := MergeIdentity(g, b.Nick, b.User, b.Realname)
	mech := b.SASLMechanism
	if mech == "" {
		mech = "PLAIN"
	}
	return gateway.Spec{
		Server: config.Server{
			Host:          b.Host,
			Port:          b.Port,
			TLS:           b.TLS,
			TLSSkipVerify: b.TLSSkipVerify,
		},
		Identity:         id,
		SASL:             config.SASL{Enabled: b.SASLEnabled, Mechanism: mech, User: b.SASLUser, Password: b.SASLPassword},
		NickServPassword: b.NickServPassword,
		Channels:         b.Channels,
	}
}

func (b *Bundle) Normalize() {
	b.Name = strings.TrimSpace(b.Name)
	b.Host = strings.TrimSpace(b.Host)
	if b.Name == "" {
		b.Name = b.Host
	}
	if b.Port == 0 {
		if b.TLS {
			b.Port = 6697
		} else {
			b.Port = 6667
		}
	}
	if strings.TrimSpace(b.SASLMechanism) == "" {
		b.SASLMechanism = "PLAIN"
	}
	b.Channels = cleanList(b.Channels)
	b.Owners = cleanList(b.Owners)
	b.Admins = cleanList(b.Admins)
}

func (b Bundle) Validate() error {
	if b.Host == "" {
		return fmt.Errorf("host is required")
	}
	if b.Port < 1 || b.Port > 65535 {
		return fmt.Errorf("port out of range")
	}
	if !b.SASLEnabled {
		return nil
	}
	mech := strings.ToUpper(strings.TrimSpace(b.SASLMechanism))
	if mech != "PLAIN" {
		return fmt.Errorf("sasl.mechanism %q not supported (PLAIN only)", b.SASLMechanism)
	}
	if strings.TrimSpace(b.SASLUser) == "" || b.SASLPassword == "" {
		return fmt.Errorf("sasl requires user and password")
	}
	if !b.TLS {
		return fmt.Errorf("sasl PLAIN requires tls")
	}
	return nil
}

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (d *DB) Count() (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM servers`).Scan(&n)
	return n, err
}

func (d *DB) ImportLegacy(c config.Config) error {
	n, err := d.Count()
	if err != nil || n > 0 || strings.TrimSpace(c.Server.Host) == "" {
		return err
	}
	b := Bundle{
		Name:             c.Server.Host,
		Host:             c.Server.Host,
		Port:             c.Server.Port,
		TLS:              c.Server.TLS,
		TLSSkipVerify:    c.Server.TLSSkipVerify,
		Enabled:          true,
		NickServPassword: c.NickServPassword,
		SASLEnabled:      c.SASL.Enabled,
		SASLMechanism:    c.SASL.Mechanism,
		SASLUser:         c.SASL.User,
		SASLPassword:     c.SASL.Password,
		Channels:         c.Channels,
		Owners:           c.Owners,
		Admins:           c.Admins,
	}
	_, err = d.Put(b)
	return err
}

func (d *DB) List() ([]Bundle, error) {
	rows, err := d.sql.Query(`SELECT id FROM servers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]Bundle, 0, len(ids))
	for _, id := range ids {
		b, err := d.Get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (d *DB) Get(id int64) (Bundle, error) {
	var b Bundle
	var tls, skip, en, sasl int
	err := d.sql.QueryRow(`
SELECT id, name, host, port, tls, tls_skip_verify, enabled, nick, user, realname,
       nickserv_password, sasl_enabled, sasl_mechanism, sasl_user, sasl_password
FROM servers WHERE id = ?`, id).Scan(
		&b.ID, &b.Name, &b.Host, &b.Port, &tls, &skip, &en, &b.Nick, &b.User, &b.Realname,
		&b.NickServPassword, &sasl, &b.SASLMechanism, &b.SASLUser, &b.SASLPassword,
	)
	if err != nil {
		return b, err
	}
	b.TLS = tls != 0
	b.TLSSkipVerify = skip != 0
	b.Enabled = en != 0
	b.SASLEnabled = sasl != 0
	b.Channels, err = d.listCol(`SELECT name FROM channels WHERE server_id = ? ORDER BY name`, id)
	if err != nil {
		return b, err
	}
	b.Owners, err = d.listCol(`SELECT mask FROM acl WHERE server_id = ? AND role = 'owner' ORDER BY mask`, id)
	if err != nil {
		return b, err
	}
	b.Admins, err = d.listCol(`SELECT mask FROM acl WHERE server_id = ? AND role = 'admin' ORDER BY mask`, id)
	return b, err
}

func (d *DB) listCol(q string, id int64) ([]string, error) {
	rows, err := d.sql.Query(q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) Put(b Bundle) (Bundle, error) {
	b.Normalize()
	if err := b.Validate(); err != nil {
		return b, err
	}
	tx, err := d.sql.Begin()
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	if b.ID == 0 {
		res, err := tx.Exec(`
INSERT INTO servers (name, host, port, tls, tls_skip_verify, enabled, nick, user, realname,
  nickserv_password, sasl_enabled, sasl_mechanism, sasl_user, sasl_password)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			b.Name, b.Host, b.Port, btoi(b.TLS), btoi(b.TLSSkipVerify), btoi(b.Enabled),
			b.Nick, b.User, b.Realname, b.NickServPassword, btoi(b.SASLEnabled), b.SASLMechanism, b.SASLUser, b.SASLPassword)
		if err != nil {
			return b, err
		}
		b.ID, err = res.LastInsertId()
		if err != nil {
			return b, err
		}
	} else {
		_, err := tx.Exec(`
UPDATE servers SET name=?, host=?, port=?, tls=?, tls_skip_verify=?, enabled=?, nick=?, user=?, realname=?,
  nickserv_password=?, sasl_enabled=?, sasl_mechanism=?, sasl_user=?, sasl_password=? WHERE id=?`,
			b.Name, b.Host, b.Port, btoi(b.TLS), btoi(b.TLSSkipVerify), btoi(b.Enabled),
			b.Nick, b.User, b.Realname, b.NickServPassword, btoi(b.SASLEnabled), b.SASLMechanism, b.SASLUser, b.SASLPassword, b.ID)
		if err != nil {
			return b, err
		}
		if _, err := tx.Exec(`DELETE FROM channels WHERE server_id=?`, b.ID); err != nil {
			return b, err
		}
		if _, err := tx.Exec(`DELETE FROM acl WHERE server_id=?`, b.ID); err != nil {
			return b, err
		}
	}
	for _, ch := range b.Channels {
		if _, err := tx.Exec(`INSERT INTO channels (server_id, name) VALUES (?,?)`, b.ID, ch); err != nil {
			return b, err
		}
	}
	for _, m := range b.Owners {
		if _, err := tx.Exec(`INSERT INTO acl (server_id, role, mask) VALUES (?,'owner',?)`, b.ID, m); err != nil {
			return b, err
		}
	}
	for _, m := range b.Admins {
		if _, err := tx.Exec(`INSERT INTO acl (server_id, role, mask) VALUES (?,'admin',?)`, b.ID, m); err != nil {
			return b, err
		}
	}
	return b, tx.Commit()
}

func (d *DB) Delete(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM servers WHERE id=?`, id)
	return err
}

func (d *DB) Connector(id int64) (Connector, error) {
	var c Connector
	err := d.sql.QueryRow(`SELECT server_id, listen, token, pid FROM connector WHERE server_id=?`, id).Scan(&c.ServerID, &c.Listen, &c.Token, &c.PID)
	return c, err
}

func (d *DB) PutConnector(c Connector) error {
	_, err := d.sql.Exec(`
INSERT INTO connector (server_id, listen, token, pid) VALUES (?,?,?,?)
ON CONFLICT(server_id) DO UPDATE SET listen=excluded.listen, token=excluded.token, pid=excluded.pid`,
		c.ServerID, c.Listen, c.Token, c.PID)
	return err
}

func (d *DB) ClearConnector(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM connector WHERE server_id=?`, id)
	return err
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

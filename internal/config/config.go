package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const DefaultPath = "config.toml"
const DefaultDatabase = "ting.db"

type Config struct {
	Database  string `toml:"database"`
	Connector string `toml:"connector"`

	// Legacy: imported into SQLite once if the DB is empty.
	Channels         []string `toml:"channels"`
	NickServPassword string   `toml:"nickserv_password"`
	Owners           []string `toml:"owners"`
	Admins           []string `toml:"admins"`
	Server           Server   `toml:"server"`
	SASL             SASL     `toml:"sasl"`

	Identity Identity `toml:"identity"`
	Control  Control  `toml:"control"`
	Admin    Admin    `toml:"admin"`
	AI       AI       `toml:"ai"`
}

type Server struct {
	Host          string `toml:"host" json:"host"`
	Port          int    `toml:"port" json:"port"`
	TLS           bool   `toml:"tls" json:"tls"`
	TLSSkipVerify bool   `toml:"tls_skip_verify" json:"tls_skip_verify"`
}

type Identity struct {
	Nick     string `toml:"nick" json:"nick"`
	User     string `toml:"user" json:"user"`
	Realname string `toml:"realname" json:"realname"`
}

type SASL struct {
	Enabled   bool   `toml:"enabled" json:"enabled"`
	Mechanism string `toml:"mechanism" json:"mechanism"`
	User      string `toml:"user" json:"user"`
	Password  string `toml:"password" json:"password"`
}

type Control struct {
	Token string `toml:"token"`
}

type Admin struct {
	Listen string `toml:"listen"`
}

// AI is the OpenRouter key. Prompt, model catalog, and sampling live in SQLite.
// Model is used only until the admin catalog is saved.
type AI struct {
	APIKey string `toml:"api_key"`
	Model  string `toml:"model"`
}

func Load(path string) (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		if os.IsNotExist(err) {
			return c, fmt.Errorf("no config at %q — copy config.example.toml to config.toml", path)
		}
		return c, err
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c *Config) applyDefaults() {
	if c.Identity.User == "" {
		c.Identity.User = c.Identity.Nick
	}
	if c.Identity.Realname == "" {
		c.Identity.Realname = c.Identity.Nick
	}
	if c.Server.Port == 0 && c.Server.Host != "" {
		if c.Server.TLS {
			c.Server.Port = 6697
		} else {
			c.Server.Port = 6667
		}
	}
	if strings.TrimSpace(c.SASL.Mechanism) == "" {
		c.SASL.Mechanism = "PLAIN"
	}
	if strings.TrimSpace(c.Admin.Listen) == "" {
		c.Admin.Listen = "127.0.0.1:42069"
	}
	if strings.TrimSpace(c.Database) == "" {
		c.Database = DefaultDatabase
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Identity.Nick) == "" {
		return fmt.Errorf("identity.nick is required")
	}
	if strings.TrimSpace(c.Control.Token) == "" {
		return fmt.Errorf("control.token is required")
	}
	return loopbackListen("admin.listen", c.Admin.Listen)
}

func loopbackListen(field, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s %q: %w", field, addr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s must be loopback, got %q", field, host)
	}
	return nil
}

func (s Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

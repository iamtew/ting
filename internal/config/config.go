package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const DefaultPath = "config.toml"

type Config struct {
	Channels         []string `toml:"channels"`
	NickServPassword string   `toml:"nickserv_password"`
	Owners           []string `toml:"owners"`
	Admins           []string `toml:"admins"`
	Server           Server   `toml:"server"`
	Identity         Identity `toml:"identity"`
	SASL             SASL     `toml:"sasl"`
	Control          Control  `toml:"control"`
	Admin            Admin    `toml:"admin"`
}

type Server struct {
	Host          string `toml:"host"`
	Port          int    `toml:"port"`
	TLS           bool   `toml:"tls"`
	TLSSkipVerify bool   `toml:"tls_skip_verify"`
}

type Identity struct {
	Nick     string `toml:"nick"`
	User     string `toml:"user"`
	Realname string `toml:"realname"`
}

type SASL struct {
	Enabled   bool   `toml:"enabled"`
	Mechanism string `toml:"mechanism"`
	User      string `toml:"user"`
	Password  string `toml:"password"`
}

type Control struct {
	Listen string `toml:"listen"`
	Token  string `toml:"token"`
}

type Admin struct {
	Listen string `toml:"listen"`
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
	if c.Server.Port == 0 {
		if c.Server.TLS {
			c.Server.Port = 6697
		} else {
			c.Server.Port = 6667
		}
	}
	if strings.TrimSpace(c.SASL.Mechanism) == "" {
		c.SASL.Mechanism = "PLAIN"
	}
	if strings.TrimSpace(c.Control.Listen) == "" {
		c.Control.Listen = "127.0.0.1:7391"
	}
	if strings.TrimSpace(c.Admin.Listen) == "" {
		c.Admin.Listen = "127.0.0.1:8080"
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.Host) == "" {
		return fmt.Errorf("server.host is required")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port out of range")
	}
	if strings.TrimSpace(c.Identity.Nick) == "" {
		return fmt.Errorf("identity.nick is required")
	}
	if strings.TrimSpace(c.Control.Token) == "" {
		return fmt.Errorf("control.token is required")
	}
	if err := loopbackListen("control.listen", c.Control.Listen); err != nil {
		return err
	}
	if err := loopbackListen("admin.listen", c.Admin.Listen); err != nil {
		return err
	}
	if !c.SASL.Enabled {
		return nil
	}
	mech := strings.ToUpper(strings.TrimSpace(c.SASL.Mechanism))
	if mech != "PLAIN" {
		return fmt.Errorf("sasl.mechanism %q not supported (PLAIN only)", c.SASL.Mechanism)
	}
	if strings.TrimSpace(c.SASL.User) == "" || c.SASL.Password == "" {
		return fmt.Errorf("sasl.enabled requires sasl.user and sasl.password")
	}
	if !c.Server.TLS {
		return fmt.Errorf("sasl PLAIN requires server.tls = true")
	}
	return nil
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

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

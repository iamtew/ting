package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const DefaultPath = "config.toml"

type Config struct {
	Channels         []string `toml:"channels"`
	NickServPassword string   `toml:"nickserv_password"`
	Server           Server   `toml:"server"`
	Identity         Identity `toml:"identity"`
	SASL             SASL     `toml:"sasl"`
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

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

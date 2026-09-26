package control

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iamtew/tng/internal/config"
	"github.com/iamtew/tng/internal/gateway"
)

func TestAPIJoinPrivmsgStatus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	got := make(chan string, 8)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		for {
			_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			got <- strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "NICK ") {
				io.WriteString(c, ":irc 001 tng :welcome\r\n")
			}
		}
	}()

	cfg := config.Config{
		Server:   config.Server{Host: "127.0.0.1", Port: addr.Port, TLS: false},
		Identity: config.Identity{Nick: "tng", User: "tng", Realname: "tng"},
		Control:  config.Control{Token: "secret"},
	}
	g := gateway.New(cfg, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go g.Run(ctx)
	waitLine(t, got, "NICK ")
	waitConnected(t, g)

	srv := httptest.NewServer(New(g, "secret", cancel, log.New(io.Discard, "", 0)).Handler())
	defer srv.Close()
	cl := NewClient(srv.URL, "secret")

	st, err := cl.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.Nick != "tng" {
		t.Fatalf("status %+v", st)
	}

	if err := cl.Join("#lab"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Privmsg("#lab", "hi"); err != nil {
		t.Fatal(err)
	}
	waitLine(t, got, "JOIN #lab")
	waitLine(t, got, "PRIVMSG #lab")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.StatusCode)
	}
}

func waitLine(t *testing.T, got <-chan string, prefix string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case line := <-got:
			if strings.HasPrefix(line, prefix) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting %q", prefix)
		}
	}
}

func waitConnected(t *testing.T, g *gateway.Gateway) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if g.Connected() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("not connected")
}

func TestStatusJSON(t *testing.T) {
	b, err := json.Marshal(Status{Nick: "tng", Connected: true, Channels: map[string][]string{}})
	if err != nil || !strings.Contains(string(b), "tng") {
		t.Fatal(err, string(b))
	}
}

package gateway

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iamtew/tng/internal/config"
)

func TestSessionJoin(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	done := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err.Error()
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		var sawJoin bool
		for i := 0; i < 20; i++ {
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, err := br.ReadString('\n')
			if err != nil {
				if !sawJoin {
					done <- err.Error()
				}
				return
			}
			if strings.HasPrefix(line, "NICK ") {
				io.WriteString(c, ":irc 001 tng :welcome\r\n")
			}
			if strings.HasPrefix(line, "JOIN ") {
				sawJoin = true
				io.WriteString(c, ":tng!~t@h JOIN :#tng\r\n")
				done <- "ok"
				return
			}
		}
		done <- "no JOIN"
	}()

	cfg := config.Config{
		Channels: []string{"#tng"},
		Server:   config.Server{Host: "127.0.0.1", Port: addr.Port, TLS: false},
		Identity: config.Identity{Nick: "tng", User: "tng", Realname: "tng"},
	}
	g := New(cfg, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go g.Run(ctx)

	select {
	case got := <-done:
		cancel()
		if got != "ok" {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

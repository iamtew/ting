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

	cfg := Spec{
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

func TestSessionErrorReconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)

	n := make(chan int, 2)
	go func() {
		for i := 1; ; i++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n <- i
			go func(c net.Conn, i int) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if strings.HasPrefix(line, "NICK ") {
						io.WriteString(c, ":irc 001 tng :welcome\r\n")
						if i == 1 {
							io.WriteString(c, "ERROR :goodbye\r\n")
						}
					}
				}
			}(c, i)
		}
	}()

	g := New(Spec{
		Server:   config.Server{Host: "127.0.0.1", Port: addr.Port, TLS: false},
		Identity: config.Identity{Nick: "tng", User: "tng", Realname: "tng"},
	}, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go g.Run(ctx)

	if (<-n) != 1 {
		t.Fatal("first accept")
	}
	select {
	case got := <-n:
		if got != 2 {
			t.Fatalf("reconnect got %d", got)
		}
	case <-ctx.Done():
		t.Fatal("no reconnect after ERROR")
	}
}

func TestSetChannelsDiff(t *testing.T) {
	g := New(Spec{Channels: []string{"#a", "#b"}}, log.New(io.Discard, "", 0))
	join, part := g.SetChannels([]string{"#b", "#c"})
	if len(join) != 1 || join[0] != "#c" || len(part) != 1 || part[0] != "#a" {
		t.Fatalf("join %v part %v", join, part)
	}
}

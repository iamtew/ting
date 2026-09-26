package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/iamtew/tng/internal/config"
)

type Spec struct {
	Server           config.Server   `json:"server"`
	Identity         config.Identity `json:"identity"`
	SASL             config.SASL     `json:"sasl"`
	NickServPassword string          `json:"nickserv_password"`
	Channels         []string        `json:"channels"`
}

func (s Spec) Addr() string { return s.Server.Addr() }

const (
	readIdle   = 3 * time.Minute
	floodDelay = 350 * time.Millisecond // ponytail: global pace; per-target tokens if a net complains
	maxBackoff = 60 * time.Second
	sendQ      = 64
	eventQ     = 64
)

// Event is one bus message. Kind is "line", "connected", or "disconnected".
type Event struct {
	Time time.Time
	Kind string
	Msg  Message
}

type Gateway struct {
	cfg    Spec
	log    *log.Logger
	events chan Event
	out    chan string

	mu        sync.RWMutex
	nick      string
	connected bool
	chans     map[string]map[string]struct{} // folded channel -> nick set
}

func New(cfg Spec, logger *log.Logger) *Gateway {
	if logger == nil {
		logger = log.Default()
	}
	return &Gateway{
		cfg:    cfg,
		log:    logger,
		events: make(chan Event, eventQ),
		out:    make(chan string, sendQ),
		nick:   cfg.Identity.Nick,
		chans:  make(map[string]map[string]struct{}),
	}
}

func (g *Gateway) Events() <-chan Event { return g.events }

func (g *Gateway) Addr() string { return g.cfg.Addr() }

func (g *Gateway) SetChannels(chs []string) (join, part []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	old := map[string]bool{}
	for _, c := range g.cfg.Channels {
		old[c] = true
	}
	neu := map[string]bool{}
	for _, c := range chs {
		neu[c] = true
		if !old[c] {
			join = append(join, c)
		}
	}
	for _, c := range g.cfg.Channels {
		if !neu[c] {
			part = append(part, c)
		}
	}
	g.cfg.Channels = append([]string(nil), chs...)
	return join, part
}

func (g *Gateway) autojoin() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]string(nil), g.cfg.Channels...)
}

func (g *Gateway) Send(line string) {
	select {
	case g.out <- line:
	default:
		g.log.Printf("send queue full, dropped %q", line)
	}
}

func (g *Gateway) Privmsg(target, text string) {
	g.Send(Message{Command: "PRIVMSG", Params: []string{target, text}}.Encode())
}

func (g *Gateway) Nick() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nick
}

func (g *Gateway) ChannelNicks(channel string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	set := g.chans[fold(channel)]
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	return out
}

func (g *Gateway) Channels() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, 0, len(g.chans))
	for ch := range g.chans {
		out = append(out, ch)
	}
	return out
}

func (g *Gateway) Connected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.connected
}

func (g *Gateway) setConnected(v bool) {
	g.mu.Lock()
	g.connected = v
	g.mu.Unlock()
}

func (g *Gateway) Run(ctx context.Context) error {
	defer close(g.events)
	backoff := time.Second
	for {
		err := g.session(ctx)
		up := g.Connected()
		g.setConnected(false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if up {
			backoff = time.Second
		}
		g.emit(Event{Kind: "disconnected"})
		g.log.Printf("disconnected: %v — retry in %s", err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (g *Gateway) session(ctx context.Context) error {
	conn, err := g.dial(ctx)
	if err != nil {
		return err
	}
	drainOut(g.out)
	g.resetMaps()
	sess, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.writeLoop(sess, conn)
	}()
	go func() {
		select {
		case <-ctx.Done():
			g.Send("QUIT :tng")
			time.Sleep(200 * time.Millisecond)
		case <-sess.Done():
		}
		conn.Close()
	}()
	defer func() {
		cancel()
		conn.Close()
		<-done
	}()

	if err := g.register(); err != nil {
		return err
	}

	br := bufio.NewReaderSize(conn, 8192)
	idlePing := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(readIdle)); err != nil {
			return err
		}
		line, err := br.ReadString('\n')
		if err != nil {
			if isTimeout(err) && !idlePing {
				idlePing = true
				g.Send("PING :tng")
				continue
			}
			return err
		}
		idlePing = false
		msg := Parse(line)
		if msg.Command == "" {
			continue
		}
		if err := g.handle(msg); err != nil {
			return err
		}
		g.emit(Event{Kind: "line", Msg: msg})
	}
}

func (g *Gateway) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 30 * time.Second}
	addr := g.cfg.Addr()
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}
	if !g.cfg.Server.TLS {
		return raw, nil
	}
	tc := &tls.Config{
		ServerName:         g.cfg.Server.Host,
		InsecureSkipVerify: g.cfg.Server.TLSSkipVerify, // ponytail: lab self-signed; keep verify on real nets
	}
	if g.cfg.Server.TLSSkipVerify {
		g.log.Printf("warning: tls_skip_verify — not verifying %s", addr)
	}
	c := tls.Client(raw, tc)
	if err := c.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return c, nil
}

func (g *Gateway) register() error {
	if g.cfg.SASL.Enabled {
		g.Send("CAP LS")
	}
	g.Send("NICK " + g.cfg.Identity.Nick)
	g.Send(fmt.Sprintf("USER %s 0 * :%s", g.cfg.Identity.User, g.cfg.Identity.Realname))
	return nil
}

func (g *Gateway) handle(msg Message) error {
	switch msg.Command {
	case "PING":
		g.Send("PONG :" + msg.Last())
	case "ERROR":
		return fmt.Errorf("irc: %s", msg.Last())
	case "CAP":
		g.handleCAP(msg)
	case "AUTHENTICATE":
		if len(msg.Params) > 0 && msg.Params[0] == "+" && g.cfg.SASL.Enabled {
			g.Send("AUTHENTICATE " + saslPlain(g.cfg.SASL.User, g.cfg.SASL.Password))
		}
	case "903": // RPL_SASLSUCCESS
		g.Send("CAP END")
	case "904", "905", "906":
		g.log.Printf("SASL failed (%s %s) — CAP END, NickServ may still run", msg.Command, msg.Last())
		g.Send("CAP END")
	case "001":
		if len(msg.Params) > 0 {
			g.mu.Lock()
			g.nick = msg.Params[0]
			g.mu.Unlock()
		}
		g.setConnected(true)
		g.emit(Event{Kind: "connected", Msg: msg})
		if p := g.cfg.NickServPassword; p != "" {
			g.Privmsg("NickServ", "IDENTIFY "+p)
		}
		for _, ch := range g.autojoin() {
			g.Send("JOIN " + ch)
		}
	case "433": // nick in use
		g.mu.Lock()
		g.nick = g.nick + "_"
		n := g.nick
		g.mu.Unlock()
		g.Send("NICK " + n)
	case "JOIN":
		g.onJoin(msg)
	case "PART", "KICK":
		g.onLeave(msg)
	case "QUIT":
		g.onQuit(msg.Nick)
	case "NICK":
		g.onNick(msg.Nick, msg.Last())
	case "353":
		g.onNames(msg)
	}
	return nil
}

func (g *Gateway) handleCAP(msg Message) {
	if !g.cfg.SASL.Enabled {
		return
	}
	sub := ""
	if len(msg.Params) >= 2 {
		sub = strings.ToUpper(msg.Params[1])
	}
	switch sub {
	case "LS":
		if strings.Contains(strings.ToLower(msg.Last()), "sasl") {
			g.Send("CAP REQ :sasl")
		} else {
			g.log.Printf("server has no sasl — CAP END")
			g.Send("CAP END")
		}
	case "ACK":
		g.Send("AUTHENTICATE PLAIN")
	case "NAK":
		g.Send("CAP END")
	}
}

func (g *Gateway) onJoin(msg Message) {
	ch := fold(msg.Last())
	if ch == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.chans[ch] == nil {
		g.chans[ch] = make(map[string]struct{})
	}
	nick := msg.Nick
	if nick == "" {
		nick = g.nick
	}
	g.chans[ch][nick] = struct{}{}
}

func (g *Gateway) onLeave(msg Message) {
	ch := ""
	nick := msg.Nick
	if msg.Command == "KICK" && len(msg.Params) >= 2 {
		ch = fold(msg.Params[0])
		nick = msg.Params[1]
	} else if len(msg.Params) > 0 {
		ch = fold(msg.Params[0])
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if nick == g.nick {
		delete(g.chans, ch)
		return
	}
	if set := g.chans[ch]; set != nil {
		delete(set, nick)
	}
}

func (g *Gateway) onQuit(nick string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, set := range g.chans {
		delete(set, nick)
	}
}

func (g *Gateway) onNick(old, neu string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if old == g.nick {
		g.nick = neu
	}
	for _, set := range g.chans {
		if _, ok := set[old]; ok {
			delete(set, old)
			set[neu] = struct{}{}
		}
	}
}

func (g *Gateway) onNames(msg Message) {
	if len(msg.Params) < 4 && msg.Last() == "" {
		return
	}
	ch := ""
	if len(msg.Params) >= 3 {
		ch = fold(msg.Params[2])
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.chans[ch] == nil {
		g.chans[ch] = make(map[string]struct{})
	}
	for _, n := range strings.Fields(msg.Last()) {
		g.chans[ch][stripStatus(n)] = struct{}{}
	}
}

func (g *Gateway) resetMaps() {
	g.mu.Lock()
	g.nick = g.cfg.Identity.Nick
	g.chans = make(map[string]map[string]struct{})
	g.mu.Unlock()
}

func (g *Gateway) emit(ev Event) {
	ev.Time = time.Now()
	select {
	case g.events <- ev:
	default:
		g.log.Printf("event bus full, dropped %s %s", ev.Kind, ev.Msg.Command)
	}
}

func (g *Gateway) writeLoop(ctx context.Context, conn net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-g.out:
			if !strings.HasSuffix(line, "\r\n") {
				line += "\r\n"
			}
			_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := io.WriteString(conn, line); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(floodDelay):
			}
		}
	}
}

func drainOut(ch <-chan string) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

func saslPlain(user, pass string) string {
	raw := []byte{0}
	raw = append(raw, user...)
	raw = append(raw, 0)
	raw = append(raw, pass...)
	return base64.StdEncoding.EncodeToString(raw)
}

package control

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iamtew/tng/internal/gateway"
)

type Status struct {
	Connected bool                `json:"connected"`
	Nick      string              `json:"nick"`
	Server    string              `json:"server"`
	Channels  map[string][]string `json:"channels"`
}

type Event struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Line string    `json:"line"`
}

type Server struct {
	gw       *gateway.Gateway
	token    string
	shutdown func()
	log      *log.Logger

	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func New(gw *gateway.Gateway, token string, shutdown func(), logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{
		gw:       gw,
		token:    token,
		shutdown: shutdown,
		log:      logger,
		subs:     make(map[chan Event]struct{}),
	}
	go s.relay()
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.auth(s.status))
	mux.HandleFunc("GET /events", s.auth(s.events))
	mux.HandleFunc("POST /join", s.auth(s.join))
	mux.HandleFunc("POST /part", s.auth(s.part))
	mux.HandleFunc("POST /privmsg", s.auth(s.privmsg))
	mux.HandleFunc("POST /nick", s.auth(s.nick))
	mux.HandleFunc("POST /raw", s.auth(s.raw))
	mux.HandleFunc("POST /quit", s.auth(s.quit))
	mux.HandleFunc("POST /shutdown", s.auth(s.doShutdown))
	return mux
}

func Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

func (s *Server) Serve(ln net.Listener) error {
	return http.Serve(ln, s.Handler())
}

func TokenOK(got, want string) bool {
	a := sha256.Sum256([]byte(got))
	b := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !TokenOK(got, s.token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	st := Status{
		Connected: s.gw.Connected(),
		Nick:      s.gw.Nick(),
		Server:    s.gw.Addr(),
		Channels:  map[string][]string{},
	}
	for _, ch := range s.gw.Channels() {
		st.Channels[ch] = s.gw.ChannelNicks(ch)
	}
	writeJSON(w, st)
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Channel) == "" {
		http.Error(w, "channel required", http.StatusBadRequest)
		return
	}
	s.gw.Send("JOIN " + req.Channel)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) part(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Reason  string `json:"reason"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Channel) == "" {
		http.Error(w, "channel required", http.StatusBadRequest)
		return
	}
	line := "PART " + req.Channel
	if req.Reason != "" {
		line += " :" + req.Reason
	}
	s.gw.Send(line)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) privmsg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
		Text   string `json:"text"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Target) == "" {
		http.Error(w, "target required", http.StatusBadRequest)
		return
	}
	s.gw.Privmsg(req.Target, req.Text)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) nick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nick string `json:"nick"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Nick) == "" {
		http.Error(w, "nick required", http.StatusBadRequest)
		return
	}
	s.gw.Send("NICK " + req.Nick)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) raw(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line string `json:"line"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Line) == "" {
		http.Error(w, "line required", http.StatusBadRequest)
		return
	}
	s.gw.Send(req.Line)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) quit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	}
	line := "QUIT"
	if req.Reason != "" {
		line += " :" + req.Reason
	} else {
		line += " :tng"
	}
	s.gw.Send(line)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) doShutdown(w http.ResponseWriter, _ *http.Request) {
	if s.shutdown != nil {
		s.shutdown()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := s.sub()
	defer s.unsub(ch)
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

func (s *Server) relay() {
	for ev := range s.gw.Events() {
		out := Event{Time: ev.Time, Kind: ev.Kind}
		if ev.Kind == "line" {
			out.Line = ev.Msg.Encode()
		} else {
			out.Line = ev.Kind
		}
		s.broadcast(out)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		close(ch)
	}
	s.subs = map[chan Event]struct{}{}
}

func (s *Server) sub() chan Event {
	ch := make(chan Event, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Server) unsub(ch chan Event) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
}

func (s *Server) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
			s.log.Printf("sse client slow, dropped %s", ev.Kind)
		}
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(dst); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

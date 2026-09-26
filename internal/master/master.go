package master

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/iamtew/tng/internal/acl"
	"github.com/iamtew/tng/internal/config"
	"github.com/iamtew/tng/internal/control"
	"github.com/iamtew/tng/internal/gateway"
)

const cookieName = "tng"
const logCap = 200

type Master struct {
	cfg  config.Config
	cl   *control.Client
	stop func()
	log  *log.Logger

	mu   sync.Mutex
	tail []control.Event
	subs map[chan control.Event]struct{}
}

func New(cfg config.Config, stop func(), logger *log.Logger) *Master {
	if logger == nil {
		logger = log.Default()
	}
	return &Master{
		cfg:  cfg,
		cl:   control.NewClient("http://"+cfg.Control.Listen, cfg.Control.Token),
		stop: stop,
		log:  logger,
		subs: make(map[chan control.Event]struct{}),
	}
}

func (m *Master) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", staticHandler())
	mux.HandleFunc("GET /login", m.loginGET)
	mux.HandleFunc("POST /login", m.loginPOST)
	mux.HandleFunc("GET /", m.auth(m.index))
	mux.HandleFunc("GET /api/status", m.auth(m.apiStatus))
	mux.HandleFunc("GET /api/stream", m.auth(m.apiStream))
	mux.HandleFunc("POST /api/join", m.auth(m.apiJoin))
	mux.HandleFunc("POST /api/part", m.auth(m.apiPart))
	mux.HandleFunc("POST /api/privmsg", m.auth(m.apiPrivmsg))
	mux.HandleFunc("POST /api/nick", m.auth(m.apiNick))
	mux.HandleFunc("POST /api/raw", m.auth(m.apiRaw))
	mux.HandleFunc("POST /api/quit", m.auth(m.apiQuit))
	mux.HandleFunc("POST /api/shutdown", m.auth(m.apiShutdown))
	return mux
}

func Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

func (m *Master) Serve(ln net.Listener) error {
	return http.Serve(ln, m.Handler())
}

func (m *Master) Consume(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		evs, err := m.cl.Events(ctx)
		if err != nil {
			m.log.Printf("events: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for ev := range evs {
			m.push(ev)
			m.onEvent(ev)
		}
	}
}

func (m *Master) onEvent(ev control.Event) {
	if ev.Kind != "line" {
		return
	}
	msg := gateway.Parse(ev.Line)
	if msg.Command != "PRIVMSG" {
		return
	}
	name, args, ok := ParseDot(msg.Last())
	if !ok {
		return
	}
	role := acl.Role(m.cfg.Owners, m.cfg.Admins, msg.Nick, msg.User, msg.Host)
	if role == "" {
		return
	}
	dest := ""
	if len(msg.Params) > 0 {
		dest = msg.Params[0]
	}
	st, _ := m.cl.Status()
	if msg.Nick != "" && msg.Nick == st.Nick {
		return
	}
	for _, a := range Dispatch(role, dest, msg.Nick, name, args, st) {
		if err := m.apply(a); err != nil {
			m.log.Printf("%s: %v", a.Kind, err)
		}
	}
}

func (m *Master) apply(a Act) error {
	switch a.Kind {
	case "privmsg":
		return m.cl.Privmsg(a.Target, a.Text)
	case "join":
		return m.cl.Join(a.Target)
	case "part":
		return m.cl.Part(a.Target, "")
	case "mode":
		return m.cl.Raw("MODE " + a.Target + " " + a.Text)
	case "nick":
		return m.cl.Nick(a.Text)
	case "shutdown":
		_ = m.cl.Shutdown()
		if m.stop != nil {
			m.stop()
		}
		return nil
	default:
		return nil
	}
}

func (m *Master) push(ev control.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tail = append(m.tail, ev)
	if len(m.tail) > logCap {
		m.tail = m.tail[len(m.tail)-logCap:]
	}
	for ch := range m.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (m *Master) tokenOK(r *http.Request) bool {
	if c, err := r.Cookie(cookieName); err == nil && control.TokenOK(c.Value, m.cfg.Control.Token) {
		return true
	}
	got := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(got) > len(p) && control.TokenOK(got[len(p):], m.cfg.Control.Token) {
		return true
	}
	return false
}

func (m *Master) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.tokenOK(r) {
			if r.Header.Get("Accept") == "text/event-stream" || len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (m *Master) loginGET(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(loginPage)
}

func (m *Master) loginPOST(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	tok := r.FormValue("token")
	if !control.TokenOK(tok, m.cfg.Control.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (m *Master) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexPage)
}

func (m *Master) apiStatus(w http.ResponseWriter, _ *http.Request) {
	st, err := m.cl.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, st)
}

func (m *Master) apiStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan control.Event, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	hist := append([]control.Event(nil), m.tail...)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}()
	for _, ev := range hist {
		if !writeSSE(w, fl, ev) {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if !writeSSE(w, fl, ev) {
				return
			}
		}
	}
}

func (m *Master) apiJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := m.cl.Join(req.Channel); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiPart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Reason  string `json:"reason"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := m.cl.Part(req.Channel, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiPrivmsg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
		Text   string `json:"text"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := m.cl.Privmsg(req.Target, req.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiNick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nick string `json:"nick"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := m.cl.Nick(req.Nick); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiRaw(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line string `json:"line"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := m.cl.Raw(req.Line); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiQuit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	}
	if err := m.cl.Quit(req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiShutdown(w http.ResponseWriter, _ *http.Request) {
	if err := m.cl.Shutdown(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeSSE(w http.ResponseWriter, fl http.Flusher, ev control.Event) bool {
	b, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := w.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
		return false
	}
	fl.Flush()
	return true
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

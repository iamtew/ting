package master

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/iamtew/tng/internal/control"
	"github.com/iamtew/tng/internal/store"
)

type proc struct {
	id     int64
	cmd    *exec.Cmd
	cl     *control.Client
	listen string
	spec   string
	pid    int
	cancel context.CancelFunc
}

type runtime struct {
	store.Bundle
	Running bool            `json:"running"`
	PID     int             `json:"pid,omitempty"`
	Listen  string          `json:"listen,omitempty"`
	Gateway *control.Status `json:"gateway,omitempty"`
}

func (m *Master) running(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[id] != nil
}

func (m *Master) client(id int64) (*control.Client, error) {
	if id == 0 {
		m.mu.Lock()
		defer m.mu.Unlock()
		if len(m.procs) == 1 {
			for _, p := range m.procs {
				return p.cl, nil
			}
		}
		return nil, fmt.Errorf("server_id required")
	}
	m.mu.Lock()
	p := m.procs[id]
	m.mu.Unlock()
	if p == nil {
		return nil, fmt.Errorf("server %d not running", id)
	}
	return p.cl, nil
}

func (m *Master) runtimeList() []runtime {
	if m.db == nil {
		return nil
	}
	list, err := m.db.List()
	if err != nil {
		return nil
	}
	out := make([]runtime, 0, len(list))
	for _, b := range list {
		out = append(out, m.withRuntime(b))
	}
	return out
}

func (m *Master) withRuntime(b store.Bundle) runtime {
	rt := runtime{Bundle: b}
	m.mu.Lock()
	p := m.procs[b.ID]
	m.mu.Unlock()
	if p == nil {
		return rt
	}
	rt.Running = true
	rt.Listen = p.listen
	if p.pid != 0 {
		rt.PID = p.pid
	} else if p.cmd != nil && p.cmd.Process != nil {
		rt.PID = p.cmd.Process.Pid
	}
	if st, err := p.cl.Status(); err == nil {
		rt.Gateway = &st
	}
	return rt
}

func (m *Master) StartServer(ctx context.Context, id int64) error {
	if m.running(id) {
		return nil
	}
	if m.db != nil {
		if c, err := m.db.Connector(id); err == nil && c.Listen != "" {
			if m.attach(id, c) {
				m.log.Printf("adopted server %d pid %d control %s", id, c.PID, c.Listen)
				return nil
			}
		}
	}
	if m.connector == "" {
		return fmt.Errorf("no connector binary")
	}
	b, err := m.db.Get(id)
	if err != nil {
		return err
	}
	spec := store.Spec(m.cfg.Identity, b)
	specPath := filepath.Join(os.TempDir(), fmt.Sprintf("tng-spec-%d.json", id))
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err := os.WriteFile(specPath, raw, 0600); err != nil {
		return err
	}
	listen, err := ephemeralLoopback()
	if err != nil {
		return err
	}
	tok, err := randomToken()
	if err != nil {
		return err
	}
	cmd := exec.Command(m.connector, "-listen", listen, "-token", tok, "-spec", specPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	isolateChild(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	cl := control.NewClient("http://"+listen, tok)
	if err := waitControl(ctx, cl); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	pctx, cancel := context.WithCancel(context.Background())
	p := &proc{id: id, cmd: cmd, cl: cl, listen: listen, spec: specPath, pid: cmd.Process.Pid, cancel: cancel}
	m.mu.Lock()
	m.procs[id] = p
	m.mu.Unlock()
	if err := m.db.PutConnector(store.Connector{ServerID: id, Listen: listen, Token: tok, PID: cmd.Process.Pid}); err != nil {
		m.log.Printf("persist connector %d: %v", id, err)
	}
	go m.reap(p)
	go m.consumeOne(pctx, p)
	m.log.Printf("connector server %d pid %d control %s", id, cmd.Process.Pid, listen)
	return nil
}

func (m *Master) attach(id int64, c store.Connector) bool {
	cl := control.NewClient("http://"+c.Listen, c.Token)
	if _, err := cl.Status(); err != nil {
		_ = m.db.ClearConnector(id)
		return false
	}
	pctx, cancel := context.WithCancel(context.Background())
	p := &proc{id: id, cl: cl, listen: c.Listen, pid: c.PID, cancel: cancel}
	m.mu.Lock()
	m.procs[id] = p
	m.mu.Unlock()
	go m.consumeOne(pctx, p)
	return true
}

func (m *Master) StopServer(id int64) error {
	if id == 0 {
		m.mu.Lock()
		if len(m.procs) == 1 {
			for i := range m.procs {
				id = i
			}
		}
		m.mu.Unlock()
		if id == 0 {
			return fmt.Errorf("server_id required")
		}
	}
	m.mu.Lock()
	p := m.procs[id]
	delete(m.procs, id)
	m.mu.Unlock()
	if p == nil {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	_ = p.cl.Shutdown()
	if p.cmd != nil && p.cmd.Process != nil {
		time.AfterFunc(2*time.Second, func() { _ = p.cmd.Process.Kill() })
	}
	if p.spec != "" {
		_ = os.Remove(p.spec)
	}
	if m.db != nil {
		_ = m.db.ClearConnector(id)
	}
	return nil
}

func (m *Master) CycleServer(ctx context.Context, id int64) error {
	_ = m.StopServer(id)
	return m.StartServer(ctx, id)
}

func (m *Master) reap(p *proc) {
	_ = p.cmd.Wait()
	m.mu.Lock()
	cur := m.procs[p.id]
	if cur == p {
		delete(m.procs, p.id)
	}
	m.mu.Unlock()
}

func (m *Master) consumeOne(ctx context.Context, p *proc) {
	for {
		if ctx.Err() != nil {
			return
		}
		evs, err := p.cl.Events(ctx)
		if err != nil {
			m.log.Printf("events %d: %v", p.id, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for ev := range evs {
			le := logEvent{ServerID: p.id, Time: ev.Time, Kind: ev.Kind, Line: ev.Line}
			m.push(le)
			m.onEvent(p.id, p.cl, ev)
		}
	}
}

func waitControl(ctx context.Context, cl *control.Client) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := cl.Status(); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("connector control did not come up")
}

func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func ephemeralLoopback() (string, error) {
	// ponytail: bind-and-close then reuse the port; collide only if something snatches it first
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

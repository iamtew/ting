package master

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/iamtew/ting/internal/ai"
	"github.com/iamtew/ting/internal/control"
	"github.com/iamtew/ting/internal/resolve"
	"github.com/iamtew/ting/internal/store"
)

const aiFailLine = "Couldn't reach the model. Try again in a moment."

// ponytail: one ring for every channel, dropped on restart. A message log replaces this.
type chatMem struct {
	mu  sync.Mutex
	buf map[string][]ai.Turn
}

func (c *chatMem) key(server int64, channel string) string {
	return strings.ToLower(channel) + "\n" + strconv.FormatInt(server, 10)
}

func (c *chatMem) add(server int64, channel string, t ai.Turn) {
	t.Content = strings.TrimSpace(t.Content)
	if t.Content == "" || !isChan(channel) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf == nil {
		c.buf = map[string][]ai.Turn{}
	}
	k := c.key(server, channel)
	turns := append(c.buf[k], t)
	if len(turns) > store.MaxAIMemoryWindow {
		turns = turns[len(turns)-store.MaxAIMemoryWindow:]
	}
	c.buf[k] = turns
}

func (c *chatMem) recent(server int64, channel string, n int) []ai.Turn {
	if n <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	turns := c.buf[c.key(server, channel)]
	if len(turns) > n {
		turns = turns[len(turns)-n:]
	}
	out := make([]ai.Turn, len(turns))
	copy(out, turns)
	return out
}

func ctcp(text string) bool {
	return strings.HasPrefix(text, "\x01")
}

func isNickChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || strings.ContainsRune("-_[]\\`^{}|", rune(b))
}

func findNick(nick, text string) (int, int, bool) {
	n := len(nick)
	if n == 0 || len(text) < n {
		return 0, 0, false
	}
	for i := 0; i+n <= len(text); i++ {
		if !strings.EqualFold(text[i:i+n], nick) {
			continue
		}
		if i > 0 && isNickChar(text[i-1]) {
			continue
		}
		if i+n < len(text) && isNickChar(text[i+n]) {
			continue
		}
		return i, i + n, true
	}
	return 0, 0, false
}

func nickMentioned(nick, text string) bool {
	_, _, ok := findNick(strings.TrimSpace(nick), text)
	return ok
}

func stripNick(nick, text string) string {
	nick = strings.TrimSpace(nick)
	for {
		start, end, ok := findNick(nick, text)
		if !ok {
			break
		}
		rest := text[end:]
		rest = strings.TrimLeft(rest, ":,")
		text = strings.TrimSpace(text[:start] + " " + rest)
	}
	return strings.TrimSpace(text)
}

func (m *Master) noteOrReply(id int64, cl *control.Client, channel, nick, botNick, text string) {
	if nickMentioned(botNick, text) {
		m.replyAI(id, cl, channel, botNick, nick, text, true)
		return
	}
	m.mem.add(id, channel, ai.Turn{Role: "user", Content: nick + ": " + text})
}

func (m *Master) replyAI(id int64, cl *control.Client, target, botNick, speaker, text string, channel bool) {
	if m.db == nil || cl == nil || strings.TrimSpace(target) == "" {
		return
	}
	userText := strings.TrimSpace(text)
	if channel {
		userText = stripNick(botNick, text)
	}
	if userText == "" {
		userText = "hey"
	}
	var hist []ai.Turn
	if channel {
		on, err := m.db.AIMemoryEnabled()
		if err != nil {
			m.log.Printf("ai memory: %v", err)
		} else if on {
			n, err := m.db.AIMemoryWindow()
			if err != nil {
				m.log.Printf("ai memory: %v", err)
			} else {
				hist = m.mem.recent(id, target, n)
			}
		}
		m.mem.add(id, target, ai.Turn{Role: "user", Content: speaker + ": " + text})
	}
	go m.finishAI(id, cl, target, userText, hist, channel)
}

func (m *Master) finishAI(id int64, cl *control.Client, target, userText string, hist []ai.Turn, channel bool) {
	on, err := m.db.AIEnabled()
	if err != nil {
		m.log.Printf("ai: %v", err)
		return
	}
	if !on {
		return
	}
	if m.aiClient == nil || !m.aiClient.Configured() {
		m.aiOnce.Do(func() {
			m.log.Printf("ai off — set ai.api_key in config.toml")
		})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ai.ChatTimeout)
	defer cancel()
	prompt, err := m.db.AISystemPrompt()
	if err != nil {
		m.log.Printf("ai prompt: %v", err)
		return
	}
	sampling, err := m.db.LoadAISampling()
	if err != nil {
		m.log.Printf("ai sampling: %v", err)
		return
	}
	catalog, err := m.db.LoadAIModels(m.cfg.AI.Model)
	if err != nil {
		m.log.Printf("ai model: %v", err)
		return
	}
	if !channel {
		hist = nil
	}
	reply, err := m.aiClient.Chat(ctx, prompt, hist, userText, catalog.Model, sampling)
	if err != nil {
		m.log.Printf("ai: %v", err)
		if err := cl.Privmsg(target, aiFailLine); err != nil {
			m.log.Printf("ai: %v", err)
		}
		return
	}
	reply = resolve.TrimIRC(reply)
	if reply == "" {
		return
	}
	if channel {
		m.mem.add(id, target, ai.Turn{Role: "assistant", Content: reply})
	}
	if err := cl.Privmsg(target, reply); err != nil {
		m.log.Printf("ai: %v", err)
	}
}

type aiSettingsBody struct {
	SystemPrompt  *string          `json:"system_prompt"`
	Enabled       *bool            `json:"enabled"`
	Sampling      *json.RawMessage `json:"sampling"`
	Model         *string          `json:"model"`
	Models        *[]string        `json:"models"`
	MemoryEnabled *bool            `json:"memory_enabled"`
	MemoryWindow  *int             `json:"memory_window"`
}

func (m *Master) apiAIGet(w http.ResponseWriter, _ *http.Request) {
	m.writeAI(w)
}

func (m *Master) apiAIPut(w http.ResponseWriter, r *http.Request) {
	if m.db == nil {
		http.Error(w, "no database", http.StatusInternalServerError)
		return
	}
	var body aiSettingsBody
	if !readJSON(w, r, &body) {
		return
	}
	if body.SystemPrompt == nil && body.Enabled == nil && body.Sampling == nil && body.Model == nil && body.Models == nil && body.MemoryEnabled == nil && body.MemoryWindow == nil {
		http.Error(w, "nothing to save", http.StatusBadRequest)
		return
	}
	if body.SystemPrompt != nil {
		prompt := strings.TrimSpace(*body.SystemPrompt)
		if utf8.RuneCountInString(prompt) > store.MaxAISystemPromptRunes {
			http.Error(w, "system prompt is too long", http.StatusBadRequest)
			return
		}
		if err := m.db.SetAISystemPrompt(prompt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.Enabled != nil {
		if err := m.db.SetAIEnabled(*body.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.Sampling != nil {
		parsed, err := ai.ParseSamplingJSON(*body.Sampling)
		if err != nil {
			http.Error(w, "invalid sampling", http.StatusBadRequest)
			return
		}
		if _, err := m.db.SaveAISampling(parsed); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.Model != nil || body.Models != nil {
		cur, err := m.db.LoadAIModels(m.cfg.AI.Model)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if body.Models != nil {
			cur.Models = *body.Models
		}
		if body.Model != nil {
			cur.Model = *body.Model
		}
		if _, err := m.db.SaveAIModels(cur); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if body.MemoryEnabled != nil {
		if err := m.db.SetAIMemoryEnabled(*body.MemoryEnabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.MemoryWindow != nil {
		if err := m.db.SetAIMemoryWindow(*body.MemoryWindow); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	m.writeAI(w)
}

func (m *Master) writeAI(w http.ResponseWriter) {
	if m.db == nil {
		http.Error(w, "no database", http.StatusInternalServerError)
		return
	}
	prompt, err := m.db.AISystemPrompt()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enabled, err := m.db.AIEnabled()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sampling, err := m.db.LoadAISampling()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	catalog, err := m.db.LoadAIModels(m.cfg.AI.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	memoryOn, err := m.db.AIMemoryEnabled()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	memoryWindow, err := m.db.AIMemoryWindow()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"ok":             true,
		"model":          catalog.Model,
		"models":         catalog.Models,
		"configured":     m.aiClient != nil && m.aiClient.Configured(),
		"enabled":        enabled,
		"system_prompt":  prompt,
		"sampling":       sampling,
		"memory_enabled": memoryOn,
		"memory_window":  memoryWindow,
	})
}

func (m *Master) apiAIChat(w http.ResponseWriter, r *http.Request) {
	if m.db == nil {
		http.Error(w, "no database", http.StatusInternalServerError)
		return
	}
	if m.aiClient == nil || !m.aiClient.Configured() {
		http.Error(w, "ai.api_key is not set", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	msg := strings.TrimSpace(body.Message)
	if msg == "" {
		http.Error(w, "message is empty", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), ai.ChatTimeout)
	defer cancel()
	prompt, err := m.db.AISystemPrompt()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sampling, err := m.db.LoadAISampling()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	catalog, err := m.db.LoadAIModels(m.cfg.AI.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reply, err := m.aiClient.Chat(ctx, prompt, nil, msg, catalog.Model, sampling)
	if err != nil {
		m.log.Printf("ai test: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"reply": reply})
}

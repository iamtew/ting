package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatMissingKey(t *testing.T) {
	_, err := New("").Chat(context.Background(), "sys", nil, "hi", DefaultModel, DefaultSampling())
	if err == nil || !strings.Contains(err.Error(), "ai.api_key") {
		t.Fatalf("err %v", err)
	}
}

func TestChatPostsTurns(t *testing.T) {
	var got chatRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		if r.Header.Get("X-Title") != "TiNG" {
			t.Errorf("title %q", r.Header.Get("X-Title"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"yo"}}]}`))
	}))
	defer srv.Close()

	c := New("secret")
	c.baseURL = srv.URL
	c.http = srv.Client()
	reply, err := c.Chat(context.Background(), "be brief", []Turn{
		{Role: "assistant", Content: "earlier"},
		{Role: "nope", Content: "from them"},
	}, "hi", "test/model", DefaultSampling())
	if err != nil {
		t.Fatal(err)
	}
	if reply != "yo" {
		t.Fatalf("reply %q", reply)
	}
	if auth != "Bearer secret" {
		t.Fatalf("auth %q", auth)
	}
	if got.Model != "test/model" || len(got.Messages) != 4 {
		t.Fatalf("%+v", got)
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "be brief" {
		t.Fatalf("system %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "assistant" || got.Messages[1].Content != "earlier" {
		t.Fatalf("hist %+v", got.Messages[1])
	}
	if got.Messages[2].Role != "user" || got.Messages[2].Content != "from them" {
		t.Fatalf("hist %+v", got.Messages[2])
	}
	if got.Messages[3].Role != "user" || got.Messages[3].Content != "hi" {
		t.Fatalf("user %+v", got.Messages[3])
	}
}

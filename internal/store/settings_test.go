package store

import (
	"path/filepath"
	"testing"
)

func TestAISystemPromptDefaultAndSave(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	got, err := d.AISystemPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultAISystemPrompt {
		t.Fatalf("default %q", got)
	}
	if err := d.SetAISystemPrompt("keep it short"); err != nil {
		t.Fatal(err)
	}
	got, err = d.AISystemPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if got != "keep it short" {
		t.Fatalf("saved %q", got)
	}
	if err := d.SetAISystemPrompt("   "); err != nil {
		t.Fatal(err)
	}
	got, err = d.AISystemPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultAISystemPrompt {
		t.Fatalf("blank save %q", got)
	}
}

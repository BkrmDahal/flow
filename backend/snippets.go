package backend

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Snippet holds a trigger → expansion pair for text replacement during dictation.
type Snippet struct {
	ID        string `json:"id"`
	Trigger   string `json:"trigger"`
	Expansion string `json:"expansion"`
	CreatedAt int64  `json:"createdAt"`
}

var snippetsMu sync.Mutex

func (a *App) snippetsFile() string {
	return filepath.Join(a.baseDir, "snippets.json")
}

func (a *App) readSnippets() ([]Snippet, error) {
	data, err := os.ReadFile(a.snippetsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return []Snippet{}, nil
		}
		return nil, fmt.Errorf("read snippets: %w", err)
	}
	if len(data) == 0 {
		return []Snippet{}, nil
	}
	var snippets []Snippet
	if err := json.Unmarshal(data, &snippets); err != nil {
		return nil, fmt.Errorf("parse snippets: %w", err)
	}
	return snippets, nil
}

func (a *App) writeSnippets(snippets []Snippet) error {
	data, err := json.MarshalIndent(snippets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snippets: %w", err)
	}
	if err := os.WriteFile(a.snippetsFile(), data, 0o644); err != nil {
		return fmt.Errorf("write snippets: %w", err)
	}
	return nil
}

// ─── Exported Wails Methods ───

// ListSnippets returns all saved snippets.
func (a *App) ListSnippets() ([]Snippet, error) {
	snippetsMu.Lock()
	defer snippetsMu.Unlock()
	return a.readSnippets()
}

// AddSnippet creates a new snippet and returns it.
func (a *App) AddSnippet(trigger string, expansion string) (Snippet, error) {
	trigger = strings.TrimSpace(trigger)
	expansion = strings.TrimSpace(expansion)
	if trigger == "" {
		return Snippet{}, fmt.Errorf("trigger cannot be empty")
	}
	if expansion == "" {
		return Snippet{}, fmt.Errorf("expansion cannot be empty")
	}

	snippetsMu.Lock()
	defer snippetsMu.Unlock()

	snippets, err := a.readSnippets()
	if err != nil {
		return Snippet{}, err
	}

	now := time.Now().UnixMilli()
	s := Snippet{
		ID:        fmt.Sprintf("snip-%d", now),
		Trigger:   trigger,
		Expansion: expansion,
		CreatedAt: now,
	}

	snippets = append(snippets, s)
	if err := a.writeSnippets(snippets); err != nil {
		return Snippet{}, err
	}

	log.Printf("[snippets] added: %q → %q", trigger, expansion)
	return s, nil
}

// UpdateSnippet updates an existing snippet by ID.
func (a *App) UpdateSnippet(id string, trigger string, expansion string) error {
	trigger = strings.TrimSpace(trigger)
	expansion = strings.TrimSpace(expansion)
	if trigger == "" {
		return fmt.Errorf("trigger cannot be empty")
	}
	if expansion == "" {
		return fmt.Errorf("expansion cannot be empty")
	}

	snippetsMu.Lock()
	defer snippetsMu.Unlock()

	snippets, err := a.readSnippets()
	if err != nil {
		return err
	}

	found := false
	for i, s := range snippets {
		if s.ID == id {
			snippets[i].Trigger = trigger
			snippets[i].Expansion = expansion
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("snippet %s not found", id)
	}

	if err := a.writeSnippets(snippets); err != nil {
		return err
	}

	log.Printf("[snippets] updated %s: %q → %q", id, trigger, expansion)
	return nil
}

// DeleteSnippet removes a snippet by ID.
func (a *App) DeleteSnippet(id string) error {
	snippetsMu.Lock()
	defer snippetsMu.Unlock()

	snippets, err := a.readSnippets()
	if err != nil {
		return err
	}

	filtered := make([]Snippet, 0, len(snippets))
	found := false
	for _, s := range snippets {
		if s.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, s)
	}

	if !found {
		return fmt.Errorf("snippet %s not found", id)
	}

	if err := a.writeSnippets(filtered); err != nil {
		return err
	}

	log.Printf("[snippets] deleted %s", id)
	return nil
}

// ApplySnippets replaces all snippet triggers in the given text with their expansions.
// Matching is case-insensitive.
func (a *App) ApplySnippets(text string) string {
	snippetsMu.Lock()
	defer snippetsMu.Unlock()

	snippets, err := a.readSnippets()
	if err != nil {
		log.Printf("[snippets] failed to load for replacement: %v", err)
		return text
	}

	if len(snippets) == 0 {
		return text
	}

	result := text
	for _, s := range snippets {
		if s.Trigger == "" {
			continue
		}
		result = replaceAllCaseInsensitive(result, s.Trigger, s.Expansion)
	}

	if result != text {
		log.Printf("[snippets] applied replacements: %d chars → %d chars", len(text), len(result))
	}
	return result
}

func replaceAllCaseInsensitive(s, old, replacement string) string {
	if old == "" {
		return s
	}

	lower := strings.ToLower(s)
	oldLower := strings.ToLower(old)

	var b strings.Builder
	b.Grow(len(s))

	start := 0
	for {
		idx := strings.Index(lower[start:], oldLower)
		if idx < 0 {
			b.WriteString(s[start:])
			break
		}
		b.WriteString(s[start : start+idx])
		b.WriteString(replacement)
		start += idx + len(old)
	}

	return b.String()
}

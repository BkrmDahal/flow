// Package snippets manages trigger→expansion text-replacement pairs.
// All CRUD operations are mutex-protected and file-backed via snippets.json.
package snippets

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

// Store encapsulates snippet CRUD operations.
type Store struct {
	baseDir string
	mu      sync.Mutex
}

// NewStore creates a new snippet store rooted at the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) filePath() string {
	return filepath.Join(s.baseDir, "snippets.json")
}

func (s *Store) read() ([]Snippet, error) {
	data, err := os.ReadFile(s.filePath())
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

func (s *Store) write(snippets []Snippet) error {
	data, err := json.MarshalIndent(snippets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snippets: %w", err)
	}
	if err := os.WriteFile(s.filePath(), data, 0o644); err != nil {
		return fmt.Errorf("write snippets: %w", err)
	}
	return nil
}

// List returns all saved snippets.
func (s *Store) List() ([]Snippet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Add creates a new snippet and returns it.
func (s *Store) Add(trigger string, expansion string) (Snippet, error) {
	trigger = strings.TrimSpace(trigger)
	expansion = strings.TrimSpace(expansion)
	if trigger == "" {
		return Snippet{}, fmt.Errorf("trigger cannot be empty")
	}
	if expansion == "" {
		return Snippet{}, fmt.Errorf("expansion cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	snippets, err := s.read()
	if err != nil {
		return Snippet{}, err
	}

	now := time.Now().UnixMilli()
	sn := Snippet{
		ID:        fmt.Sprintf("snip-%d", now),
		Trigger:   trigger,
		Expansion: expansion,
		CreatedAt: now,
	}

	snippets = append(snippets, sn)
	if err := s.write(snippets); err != nil {
		return Snippet{}, err
	}

	log.Printf("[snippets] added: %q → %q", trigger, expansion)
	return sn, nil
}

// Update updates an existing snippet by ID.
func (s *Store) Update(id string, trigger string, expansion string) error {
	trigger = strings.TrimSpace(trigger)
	expansion = strings.TrimSpace(expansion)
	if trigger == "" {
		return fmt.Errorf("trigger cannot be empty")
	}
	if expansion == "" {
		return fmt.Errorf("expansion cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	snippets, err := s.read()
	if err != nil {
		return err
	}

	found := false
	for i, sn := range snippets {
		if sn.ID == id {
			snippets[i].Trigger = trigger
			snippets[i].Expansion = expansion
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("snippet %s not found", id)
	}

	if err := s.write(snippets); err != nil {
		return err
	}

	log.Printf("[snippets] updated %s: %q → %q", id, trigger, expansion)
	return nil
}

// Delete removes a snippet by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snippets, err := s.read()
	if err != nil {
		return err
	}

	filtered := make([]Snippet, 0, len(snippets))
	found := false
	for _, sn := range snippets {
		if sn.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, sn)
	}

	if !found {
		return fmt.Errorf("snippet %s not found", id)
	}

	if err := s.write(filtered); err != nil {
		return err
	}

	log.Printf("[snippets] deleted %s", id)
	return nil
}

// Apply replaces all snippet triggers in the given text with their expansions.
// Matching is case-insensitive.
func (s *Store) Apply(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	snippets, err := s.read()
	if err != nil {
		log.Printf("[snippets] failed to load for replacement: %v", err)
		return text
	}

	if len(snippets) == 0 {
		return text
	}

	result := text
	for _, sn := range snippets {
		if sn.Trigger == "" {
			continue
		}
		result = ReplaceAllCaseInsensitive(result, sn.Trigger, sn.Expansion)
	}

	if result != text {
		log.Printf("[snippets] applied replacements: %d chars → %d chars", len(text), len(result))
	}
	return result
}

// ReplaceAllCaseInsensitive replaces all occurrences of old in s with
// replacement, matching case-insensitively.
func ReplaceAllCaseInsensitive(s, old, replacement string) string {
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

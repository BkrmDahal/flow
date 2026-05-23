// Package plugins manages custom commands and skills (the "Plugins" system).
// All CRUD operations are mutex-protected and file-backed via plugins.json
// plus per-entity .md files.
package plugins

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

// Command represents a custom slash command.
type Command struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// Skill represents a reusable AI skill (injected into system prompt).
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// Data is the root structure of plugins.json.
type Data struct {
	Commands []Command `json:"commands"`
	Skills   []Skill   `json:"skills"`
}

// CommandDetail includes the markdown body of a command.
type CommandDetail struct {
	Command
	Body string `json:"body"`
}

// SkillDetail includes the markdown body of a skill.
type SkillDetail struct {
	Skill
	Body string `json:"body"`
}

// Store encapsulates all plugin CRUD operations.
type Store struct {
	baseDir string
	mu      sync.Mutex
}

// NewStore creates a new plugin store rooted at the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) jsonPath() string {
	return filepath.Join(s.baseDir, "plugins.json")
}

func (s *Store) dir(kind string) string {
	return filepath.Join(s.baseDir, "plugins", kind)
}

func (s *Store) readData() (Data, error) {
	data, err := os.ReadFile(s.jsonPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Data{Commands: []Command{}, Skills: []Skill{}}, nil
		}
		return Data{}, fmt.Errorf("read plugins.json: %w", err)
	}
	if len(data) == 0 {
		return Data{Commands: []Command{}, Skills: []Skill{}}, nil
	}
	var pd Data
	if err := json.Unmarshal(data, &pd); err != nil {
		return Data{}, fmt.Errorf("parse plugins.json: %w", err)
	}
	if pd.Commands == nil {
		pd.Commands = []Command{}
	}
	if pd.Skills == nil {
		pd.Skills = []Skill{}
	}
	return pd, nil
}

func (s *Store) writeData(pd Data) error {
	data, err := json.MarshalIndent(pd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plugins.json: %w", err)
	}
	if err := os.WriteFile(s.jsonPath(), data, 0o644); err != nil {
		return fmt.Errorf("write plugins.json: %w", err)
	}
	return nil
}

func (s *Store) readMdFile(kind, name string) (string, error) {
	dir := s.dir(kind)
	data, err := os.ReadFile(filepath.Join(dir, name+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s/%s.md: %w", kind, name, err)
	}
	return string(data), nil
}

func (s *Store) writeMdFile(kind, name, body string) error {
	dir := s.dir(kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644)
}

func (s *Store) deleteMdFile(kind, name string) error {
	dir := s.dir(kind)
	if err := os.Remove(filepath.Join(dir, name+".md")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SanitizeName normalises a plugin name to lowercase-hyphenated form.
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		result = "untitled"
	}
	return result
}

// ─── Command CRUD ───

// ListCommands returns all saved commands.
func (s *Store) ListCommands() ([]Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return nil, err
	}
	return pd.Commands, nil
}

// GetCommand returns a command by ID with its body.
func (s *Store) GetCommand(id string) (CommandDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return CommandDetail{}, err
	}
	for _, cmd := range pd.Commands {
		if cmd.ID == id {
			body, err := s.readMdFile("commands", cmd.Name)
			if err != nil {
				return CommandDetail{}, err
			}
			return CommandDetail{Command: cmd, Body: body}, nil
		}
	}
	return CommandDetail{}, fmt.Errorf("command %s not found", id)
}

// AddCommand creates a new command and returns it.
func (s *Store) AddCommand(name string, description string, body string) (Command, error) {
	name = SanitizeName(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return Command{}, fmt.Errorf("command name cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return Command{}, err
	}
	for _, cmd := range pd.Commands {
		if cmd.Name == name {
			return Command{}, fmt.Errorf("command %q already exists", name)
		}
	}
	now := time.Now().UnixMilli()
	cmd := Command{
		ID: fmt.Sprintf("cmd-%d", now), Name: name, Description: description,
		CreatedAt: now, UpdatedAt: now,
	}
	pd.Commands = append(pd.Commands, cmd)
	if err := s.writeData(pd); err != nil {
		return Command{}, err
	}
	if err := s.writeMdFile("commands", name, body); err != nil {
		return Command{}, err
	}
	log.Printf("[plugins] added command: %s", name)
	return cmd, nil
}

// UpdateCommand updates an existing command by ID.
func (s *Store) UpdateCommand(id string, name string, description string, body string) error {
	name = SanitizeName(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return fmt.Errorf("command name cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return err
	}
	found := false
	var oldName string
	for i, cmd := range pd.Commands {
		if cmd.ID == id {
			oldName = cmd.Name
			if name != oldName {
				for _, other := range pd.Commands {
					if other.ID != id && other.Name == name {
						return fmt.Errorf("command %q already exists", name)
					}
				}
			}
			pd.Commands[i].Name = name
			pd.Commands[i].Description = description
			pd.Commands[i].UpdatedAt = time.Now().UnixMilli()
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("command %s not found", id)
	}
	if err := s.writeData(pd); err != nil {
		return err
	}
	if oldName != name {
		_ = s.deleteMdFile("commands", oldName)
	}
	if err := s.writeMdFile("commands", name, body); err != nil {
		return err
	}
	log.Printf("[plugins] updated command %s: %s", id, name)
	return nil
}

// DeleteCommand removes a command by ID.
func (s *Store) DeleteCommand(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return err
	}
	filtered := make([]Command, 0, len(pd.Commands))
	var deletedName string
	for _, cmd := range pd.Commands {
		if cmd.ID == id {
			deletedName = cmd.Name
			continue
		}
		filtered = append(filtered, cmd)
	}
	if deletedName == "" {
		return fmt.Errorf("command %s not found", id)
	}
	pd.Commands = filtered
	if err := s.writeData(pd); err != nil {
		return err
	}
	_ = s.deleteMdFile("commands", deletedName)
	log.Printf("[plugins] deleted command %s (%s)", id, deletedName)
	return nil
}

// ─── Skill CRUD ───

// ListSkills returns all saved skills.
func (s *Store) ListSkills() ([]Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return nil, err
	}
	return pd.Skills, nil
}

// GetSkill returns a skill by ID with its body.
func (s *Store) GetSkill(id string) (SkillDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return SkillDetail{}, err
	}
	for _, sk := range pd.Skills {
		if sk.ID == id {
			body, err := s.readMdFile("skills", sk.Name)
			if err != nil {
				return SkillDetail{}, err
			}
			return SkillDetail{Skill: sk, Body: body}, nil
		}
	}
	return SkillDetail{}, fmt.Errorf("skill %s not found", id)
}

// AddSkill creates a new skill and returns it.
func (s *Store) AddSkill(name string, description string, body string) (Skill, error) {
	name = SanitizeName(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return Skill{}, fmt.Errorf("skill name cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return Skill{}, err
	}
	for _, sk := range pd.Skills {
		if sk.Name == name {
			return Skill{}, fmt.Errorf("skill %q already exists", name)
		}
	}
	now := time.Now().UnixMilli()
	sk := Skill{
		ID: fmt.Sprintf("skill-%d", now), Name: name, Description: description,
		CreatedAt: now, UpdatedAt: now,
	}
	pd.Skills = append(pd.Skills, sk)
	if err := s.writeData(pd); err != nil {
		return Skill{}, err
	}
	if err := s.writeMdFile("skills", name, body); err != nil {
		return Skill{}, err
	}
	log.Printf("[plugins] added skill: %s", name)
	return sk, nil
}

// UpdateSkill updates an existing skill by ID.
func (s *Store) UpdateSkill(id string, name string, description string, body string) error {
	name = SanitizeName(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return fmt.Errorf("skill name cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return err
	}
	found := false
	var oldName string
	for i, sk := range pd.Skills {
		if sk.ID == id {
			oldName = sk.Name
			if name != oldName {
				for _, other := range pd.Skills {
					if other.ID != id && other.Name == name {
						return fmt.Errorf("skill %q already exists", name)
					}
				}
			}
			pd.Skills[i].Name = name
			pd.Skills[i].Description = description
			pd.Skills[i].UpdatedAt = time.Now().UnixMilli()
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("skill %s not found", id)
	}
	if err := s.writeData(pd); err != nil {
		return err
	}
	if oldName != name {
		_ = s.deleteMdFile("skills", oldName)
	}
	if err := s.writeMdFile("skills", name, body); err != nil {
		return err
	}
	log.Printf("[plugins] updated skill %s: %s", id, name)
	return nil
}

// DeleteSkill removes a skill by ID.
func (s *Store) DeleteSkill(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return err
	}
	filtered := make([]Skill, 0, len(pd.Skills))
	var deletedName string
	for _, sk := range pd.Skills {
		if sk.ID == id {
			deletedName = sk.Name
			continue
		}
		filtered = append(filtered, sk)
	}
	if deletedName == "" {
		return fmt.Errorf("skill %s not found", id)
	}
	pd.Skills = filtered
	if err := s.writeData(pd); err != nil {
		return err
	}
	_ = s.deleteMdFile("skills", deletedName)
	log.Printf("[plugins] deleted skill %s (%s)", id, deletedName)
	return nil
}

// ─── Helpers for AI integration ───

// GetCommandByName returns the markdown body of a command by name.
func (s *Store) GetCommandByName(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return "", err
	}
	for _, cmd := range pd.Commands {
		if cmd.Name == name {
			return s.readMdFile("commands", cmd.Name)
		}
	}
	return "", fmt.Errorf("command %q not found", name)
}

// GetAllSkillBodies returns all skill bodies concatenated.
func (s *Store) GetAllSkillBodies() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return "", err
	}
	if len(pd.Skills) == 0 {
		return "", nil
	}
	var parts []string
	for _, sk := range pd.Skills {
		body, err := s.readMdFile("skills", sk.Name)
		if err != nil {
			log.Printf("[plugins] warning: failed to read skill %s: %v", sk.Name, err)
			continue
		}
		if body != "" {
			parts = append(parts, body)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

// ListCommandNames returns all command names (for routing).
func (s *Store) ListCommandNames() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd, err := s.readData()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(pd.Commands))
	for i, cmd := range pd.Commands {
		names[i] = cmd.Name
	}
	return names, nil
}

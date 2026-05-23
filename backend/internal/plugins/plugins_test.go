package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return NewStore(dir)
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"  spaces  ", "spaces"},
		{"UPPER_CASE", "upper_case"},
		{"special!@#chars", "specialchars"},
		{"", "untitled"},
		{"---dashes---", "dashes"},
		{"foo--bar", "foo-bar"},
	}
	for _, tt := range tests {
		got := SanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCommandCRUD(t *testing.T) {
	s := tempStore(t)

	// List empty.
	cmds, err := s.ListCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(cmds))
	}

	// Add.
	cmd, err := s.AddCommand("Test Cmd", "A test command", "## body")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name != "test-cmd" {
		t.Fatalf("expected sanitized name 'test-cmd', got %q", cmd.Name)
	}

	// Verify body was saved.
	bodyPath := filepath.Join(s.dir("commands"), "test-cmd.md")
	data, err := os.ReadFile(bodyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "## body" {
		t.Fatalf("expected body '## body', got %q", string(data))
	}

	// Get.
	detail, err := s.GetCommand(cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Body != "## body" {
		t.Fatalf("expected body '## body', got %q", detail.Body)
	}

	// Update.
	err = s.UpdateCommand(cmd.ID, "Updated Cmd", "Updated desc", "new body")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = s.GetCommand(cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Name != "updated-cmd" || detail.Body != "new body" {
		t.Fatalf("update failed: name=%q body=%q", detail.Name, detail.Body)
	}

	// Delete.
	err = s.DeleteCommand(cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	cmds, _ = s.ListCommands()
	if len(cmds) != 0 {
		t.Fatalf("expected 0 commands after delete, got %d", len(cmds))
	}
}

func TestSkillCRUD(t *testing.T) {
	s := tempStore(t)

	sk, err := s.AddSkill("My Skill", "test skill", "skill body")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "my-skill" {
		t.Fatalf("expected 'my-skill', got %q", sk.Name)
	}

	skills, _ := s.ListSkills()
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	bodies, err := s.GetAllSkillBodies()
	if err != nil {
		t.Fatal(err)
	}
	if bodies != "skill body" {
		t.Fatalf("expected 'skill body', got %q", bodies)
	}

	err = s.DeleteSkill(sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	skills, _ = s.ListSkills()
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills after delete, got %d", len(skills))
	}
}

func TestDuplicateCommandName(t *testing.T) {
	s := tempStore(t)
	_, err := s.AddCommand("Dup", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddCommand("Dup", "second", "")
	if err == nil {
		t.Fatal("expected error for duplicate command name")
	}
}

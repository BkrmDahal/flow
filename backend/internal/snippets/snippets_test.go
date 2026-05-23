package snippets

import (
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func TestSnippetCRUD(t *testing.T) {
	s := tempStore(t)

	// Empty list.
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 snippets, got %d", len(list))
	}

	// Add.
	sn, err := s.Add("brb", "be right back")
	if err != nil {
		t.Fatal(err)
	}
	if sn.Trigger != "brb" || sn.Expansion != "be right back" {
		t.Fatalf("unexpected snippet: %+v", sn)
	}

	// List.
	list, _ = s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(list))
	}

	// Update.
	err = s.Update(sn.ID, "brb", "be right back!")
	if err != nil {
		t.Fatal(err)
	}
	list, _ = s.List()
	if list[0].Expansion != "be right back!" {
		t.Fatalf("update failed: %q", list[0].Expansion)
	}

	// Delete.
	err = s.Delete(sn.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, _ = s.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(list))
	}
}

func TestSnippetAdd_Validation(t *testing.T) {
	s := tempStore(t)

	_, err := s.Add("", "expansion")
	if err == nil {
		t.Fatal("expected error for empty trigger")
	}
	_, err = s.Add("trigger", "")
	if err == nil {
		t.Fatal("expected error for empty expansion")
	}
}

func TestApply(t *testing.T) {
	s := tempStore(t)
	_, _ = s.Add("brb", "be right back")
	_, _ = s.Add("omw", "on my way")

	result := s.Apply("I'll BRB, omw to the store")
	expected := "I'll be right back, on my way to the store"
	if result != expected {
		t.Fatalf("Apply:\n  got:  %q\n  want: %q", result, expected)
	}
}

func TestReplaceAllCaseInsensitive(t *testing.T) {
	tests := []struct {
		s, old, repl, want string
	}{
		{"Hello World", "hello", "Hi", "Hi World"},
		{"aaa", "a", "bb", "bbbbbb"},
		{"no match", "xyz", "!", "no match"},
		{"", "a", "b", ""},
		{"abc", "", "x", "abc"},
	}
	for _, tt := range tests {
		got := ReplaceAllCaseInsensitive(tt.s, tt.old, tt.repl)
		if got != tt.want {
			t.Errorf("ReplaceAllCaseInsensitive(%q, %q, %q) = %q, want %q",
				tt.s, tt.old, tt.repl, got, tt.want)
		}
	}
}

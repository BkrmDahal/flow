package backend

import (
	"github.com/user/flow/backend/internal/snippets"
)

// Snippet is re-exported for Wails binding compatibility.
type Snippet = snippets.Snippet

// ─── Snippet CRUD (Wails facades) ───

func (a *App) ListSnippets() ([]Snippet, error) {
	return a.snippets.List()
}

func (a *App) AddSnippet(trigger string, expansion string) (Snippet, error) {
	return a.snippets.Add(trigger, expansion)
}

func (a *App) UpdateSnippet(id string, trigger string, expansion string) error {
	return a.snippets.Update(id, trigger, expansion)
}

func (a *App) DeleteSnippet(id string) error {
	return a.snippets.Delete(id)
}

func (a *App) ApplySnippets(text string) string {
	return a.snippets.Apply(text)
}

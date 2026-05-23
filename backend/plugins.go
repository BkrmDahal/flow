package backend

import (
	"github.com/user/flow/backend/internal/plugins"
)

// PluginCommand is re-exported for Wails binding compatibility.
type PluginCommand = plugins.Command

// PluginSkill is re-exported for Wails binding compatibility.
type PluginSkill = plugins.Skill

// PluginCommandDetail is re-exported for Wails binding compatibility.
type PluginCommandDetail = plugins.CommandDetail

// PluginSkillDetail is re-exported for Wails binding compatibility.
type PluginSkillDetail = plugins.SkillDetail

// ─── Command CRUD (Wails facades) ───

func (a *App) ListCommands() ([]PluginCommand, error) {
	return a.plugins.ListCommands()
}

func (a *App) GetCommand(id string) (PluginCommandDetail, error) {
	return a.plugins.GetCommand(id)
}

func (a *App) AddCommand(name string, description string, body string) (PluginCommand, error) {
	return a.plugins.AddCommand(name, description, body)
}

func (a *App) UpdateCommand(id string, name string, description string, body string) error {
	return a.plugins.UpdateCommand(id, name, description, body)
}

func (a *App) DeleteCommand(id string) error {
	return a.plugins.DeleteCommand(id)
}

// ─── Skill CRUD (Wails facades) ───

func (a *App) ListSkills() ([]PluginSkill, error) {
	return a.plugins.ListSkills()
}

func (a *App) GetSkill(id string) (PluginSkillDetail, error) {
	return a.plugins.GetSkill(id)
}

func (a *App) AddSkill(name string, description string, body string) (PluginSkill, error) {
	return a.plugins.AddSkill(name, description, body)
}

func (a *App) UpdateSkill(id string, name string, description string, body string) error {
	return a.plugins.UpdateSkill(id, name, description, body)
}

func (a *App) DeleteSkill(id string) error {
	return a.plugins.DeleteSkill(id)
}

// ─── Helpers for AI integration (Wails facades) ───

func (a *App) GetCommandByName(name string) (string, error) {
	return a.plugins.GetCommandByName(name)
}

func (a *App) GetAllSkillBodies() (string, error) {
	return a.plugins.GetAllSkillBodies()
}

func (a *App) ListCommandNames() ([]string, error) {
	return a.plugins.ListCommandNames()
}

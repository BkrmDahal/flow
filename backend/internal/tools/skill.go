package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UseSkillTool loads a skill's content by name. Skills are markdown files
// stored in ~/.flow/plugins/skills/{name}.md. The LLM calls this tool
// on-demand when the user's request matches an available skill.
type UseSkillTool struct {
	baseDir string
}

func NewUseSkillTool(baseDir string) *UseSkillTool {
	return &UseSkillTool{baseDir: baseDir}
}

func (t *UseSkillTool) Name() string { return "use_skill" }

func (t *UseSkillTool) Description() string {
	return "Load a skill by name to get specialized instructions and knowledge for a specific task. Skills are listed in the Available Skills section of the system prompt. Use this tool when the user's request matches a skill's description — load the skill first, then follow its instructions."
}

func (t *UseSkillTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The name of the skill to load (exactly as listed in Available Skills)",
			},
		},
		"required": []string{"name"},
	}
}

type useSkillInput struct {
	Name string `json:"name"`
}

func (t *UseSkillTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in useSkillInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "Error: skill name cannot be empty", nil
	}

	skillPath := filepath.Join(t.baseDir, "plugins", "skills", name+".md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: skill %q not found. Check the Available Skills list for valid names.", name), nil
		}
		return fmt.Sprintf("Error reading skill %q: %v", name, err), nil
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return fmt.Sprintf("Error: skill %q exists but is empty", name), nil
	}

	return content, nil
}

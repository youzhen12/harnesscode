package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"harnesscode-go/internal/agents"
)

// EnsureAgentsInstalled installs harnesscode agents for the given backend
// (opencode or claude). It is idempotent and safe to call multiple times.
func EnsureAgentsInstalled(backendName string) error {
	switch backendName {
	case "claude":
		return installClaudeAgents()
	default: // opencode as default
		return installOpenCodeAgents()
	}
}

// -----------------------------------------------------------------------------
// OpenCode (opencode.json + markdown agents)
// -----------------------------------------------------------------------------

func opencodeConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "opencode")
}

func installOpenCodeAgents() error {
	confDir := opencodeConfigDir()
	agentsDir := filepath.Join(confDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}

	// Write agent markdown files.
	for srcName, content := range agents.All() {
		var dstName string
		switch srcName {
		case "orchestrator.md":
			dstName = "harnesscode-orchestrator.md"
		case "initializer.md":
			dstName = "harnesscode-initializer.md"
		case "coder.md":
			dstName = "harnesscode-coder.md"
		case "tester.md":
			dstName = "harnesscode-tester.md"
		case "fixer.md":
			dstName = "harnesscode-fixer.md"
		case "reviewer.md":
			dstName = "harnesscode-reviewer.md"
		default:
			continue
		}
		path := filepath.Join(agentsDir, dstName)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// Merge harnesscode agent config into opencode.json (best-effort).
	return mergeOpenCodeConfig(confDir)
}

func mergeOpenCodeConfig(confDir string) error {
	confPath := filepath.Join(confDir, "opencode.json")
	var root map[string]any
	if data, err := os.ReadFile(confPath); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &root)
	}
	if root == nil {
		root = map[string]any{}
	}

	agentMap, _ := root["agent"].(map[string]any)
	if agentMap == nil {
		agentMap = map[string]any{}
		root["agent"] = agentMap
	}

	perm := map[string]any{
		"external_directory": map[string]any{
			"~/.harnesscode/*": "allow",
		},
	}

	addAgent := func(name, fileName string) {
		promptPath := fmt.Sprintf("{file:%s}", filepath.Join(opencodeConfigDir(), "agents", fileName))
		agentMap[name] = map[string]any{
			"prompt":     promptPath,
			"mode":       "primary",
			"permission": perm,
		}
	}

	addAgent("harnesscode-orchestrator", "harnesscode-orchestrator.md")
	addAgent("harnesscode-initializer", "harnesscode-initializer.md")
	addAgent("harnesscode-coder", "harnesscode-coder.md")
	addAgent("harnesscode-tester", "harnesscode-tester.md")
	addAgent("harnesscode-fixer", "harnesscode-fixer.md")
	addAgent("harnesscode-reviewer", "harnesscode-reviewer.md")

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(confPath, out, 0o644)
}

// -----------------------------------------------------------------------------
// Claude (markdown with YAML frontmatter)
// -----------------------------------------------------------------------------

func claudeConfigDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".claude")
	}
	return filepath.Join(home, ".claude")
}

func installClaudeAgents() error {
	confDir := claudeConfigDir()
	agentsDir := filepath.Join(confDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}

	for srcName, content := range agents.All() {
		var dstName, key string
		switch srcName {
		case "orchestrator.md":
			dstName = "harnesscode-orchestrator.md"
			key = "orchestrator"
		case "initializer.md":
			dstName = "harnesscode-initializer.md"
			key = "initializer"
		case "coder.md":
			dstName = "harnesscode-coder.md"
			key = "coder"
		case "tester.md":
			dstName = "harnesscode-tester.md"
			key = "tester"
		case "fixer.md":
			dstName = "harnesscode-fixer.md"
			key = "fixer"
		case "reviewer.md":
			dstName = "harnesscode-reviewer.md"
			key = "reviewer"
		default:
			continue
		}

		full := fmt.Sprintf("---\nname: harnesscode-%s\ndescription: HarnessCode %s agent\npermissionMode: bypassPermissions\n---\n\n%s", key, key, content)
		path := filepath.Join(agentsDir, dstName)
		if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
			return err
		}
	}
	return nil
}

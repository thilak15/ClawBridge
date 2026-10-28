package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clawbridge/pkg/auth"
	"gopkg.in/yaml.v3"
)

// AgentConfig represents the configuration of an autonomous agent loaded from YAML.
type AgentConfig struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Model        string   `yaml:"model,omitempty"`
	Permissions  []string `yaml:"permissions"`
}

// LoadAgentsFromDir scans a directory for agent YAML files and auto-registers them in SQLite.
func LoadAgentsFromDir(dir string, registry *auth.AgentRegistry) ([]*AgentConfig, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	var configs []*AgentConfig

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		var cfg AgentConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		if cfg.Name == "" {
			cfg.Name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if len(cfg.Permissions) == 0 {
			cfg.Permissions = []string{"*"}
		}

		// Auto-register or update rules in SQLite if registry provided
		if registry != nil {
			existing, err := registry.GetAgentByName(cfg.Name)
			if err != nil || existing == nil {
				_, _, _ = registry.RegisterAgent(cfg.Name, cfg.Permissions)
			} else {
				_ = registry.UpdateAgentRules(cfg.Name, cfg.Permissions)
			}
		}

		configs = append(configs, &cfg)
	}

	return configs, nil
}

// CreateAgentYAML creates a new agent configuration file in the specified directory.
func CreateAgentYAML(dir string, cfg AgentConfig) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create agents directory: %w", err)
	}

	if cfg.Name == "" {
		return "", fmt.Errorf("agent name cannot be empty")
	}
	if len(cfg.Permissions) == 0 {
		cfg.Permissions = []string{"*"}
	}

	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal agent yaml: %w", err)
	}

	filePath := filepath.Join(dir, fmt.Sprintf("%s.yaml", cfg.Name))
	if err := os.WriteFile(filePath, yamlData, 0644); err != nil {
		return "", fmt.Errorf("failed to write agent yaml file: %w", err)
	}

	return filePath, nil
}

// UpdateAgentYAMLPermissions updates the permissions field in an existing agent YAML file.
func UpdateAgentYAMLPermissions(dir string, name string, newPermissions []string) error {
	pathYAML := filepath.Join(dir, fmt.Sprintf("%s.yaml", name))
	pathYML := filepath.Join(dir, fmt.Sprintf("%s.yml", name))

	targetPath := pathYAML
	if _, err := os.Stat(pathYAML); os.IsNotExist(err) {
		if _, err := os.Stat(pathYML); err == nil {
			targetPath = pathYML
		}
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			_, err = CreateAgentYAML(dir, AgentConfig{
				Name:        name,
				Permissions: newPermissions,
			})
			return err
		}
		return fmt.Errorf("failed to read agent file: %w", err)
	}

	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse agent yaml: %w", err)
	}

	cfg.Permissions = newPermissions
	outData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal agent yaml: %w", err)
	}

	return os.WriteFile(targetPath, outData, 0644)
}

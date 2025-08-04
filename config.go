package main

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

var appConfig *Config

// loadConfig loads configuration from a YAML file
func loadConfig(configPath string) (*Config, error) {
	// If no config path provided, return default empty config
	if configPath == "" {
		return &Config{
			Messages: MessagesConfig{
				Groups: make(map[string][]string),
			},
			Storage: StorageConfig{
				Dir: "./storage", // Default storage directory
			},
		}, nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// Parse YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	// Initialize groups map if not present
	if config.Messages.Groups == nil {
		config.Messages.Groups = make(map[string][]string)
	}

	// Set default storage directory if not specified
	if config.Storage.Dir == "" {
		config.Storage.Dir = "./storage"
	}

	log.Printf("Configuration loaded from %s with %d message groups and storage dir: %s", configPath, len(config.Messages.Groups), config.Storage.Dir)
	return &config, nil
}

// expandGroups expands group names to their individual recipients
func expandGroups(recipients []string) []string {
	if appConfig == nil || appConfig.Messages.Groups == nil {
		return recipients
	}

	var expanded []string
	for _, recipient := range recipients {
		// Check if this recipient is a group name
		if groupMembers, exists := appConfig.Messages.Groups[recipient]; exists {
			// Add all group members
			expanded = append(expanded, groupMembers...)
			log.Printf("Expanded group '%s' to %d recipients", recipient, len(groupMembers))
		} else {
			// Not a group, add as individual recipient
			expanded = append(expanded, recipient)
		}
	}

	return expanded
} 
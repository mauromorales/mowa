package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

var appConfig *Config

// defaultConfig returns the built-in configuration used when no config file is
// present.
func defaultConfig() *Config {
	return &Config{
		Messages: MessagesConfig{
			Groups:         make(map[string][]string),
			TimeoutSeconds: defaultSendTimeoutSeconds,
		},
		Storage: StorageConfig{
			Dir: "./storage", // Default storage directory
		},
	}
}

// loadConfig loads configuration from a YAML file
func loadConfig(configPath string) (*Config, error) {
	// If no config path provided, return the default config.
	if configPath == "" {
		return defaultConfig(), nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		// A missing config file is not an error: fall back to defaults so a
		// service pointed at a not-yet-created config (e.g. the launchd agent
		// installed by `mowa install`) still starts. Other read errors such as
		// bad permissions are real and surfaced to the caller.
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("Config file %s not found; using defaults", configPath)
			return defaultConfig(), nil
		}
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

	// Set default send timeout if not specified or invalid
	if config.Messages.TimeoutSeconds <= 0 {
		config.Messages.TimeoutSeconds = defaultSendTimeoutSeconds
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
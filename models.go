package main

// Config represents the application configuration
type Config struct {
	Messages MessagesConfig `yaml:"messages"`
	Storage  StorageConfig  `yaml:"storage"`
}

// MessagesConfig represents the messages configuration
type MessagesConfig struct {
	Groups map[string][]string `yaml:"groups"`
}

// StorageConfig represents the storage configuration
type StorageConfig struct {
	Dir string `yaml:"dir"`
}

// MessageRequest represents the request to send messages
type MessageRequest struct {
	To      []string `json:"to" binding:"required"`
	Message string   `json:"message" binding:"required"`
}

// MessageResponse represents the response from sending messages
type MessageResponse struct {
	Results []MessageResult `json:"results"`
}

// MessageResult represents the result of sending a message to one recipient
type MessageResult struct {
	Recipient string  `json:"recipient"`
	Success   bool    `json:"success"`
	Error     *string `json:"error,omitempty"`
}

// UptimeResponse represents the system uptime response
type UptimeResponse struct {
	Uptime        string        `json:"uptime"`
	UptimeSeconds float64       `json:"uptimeSeconds"`
	Formatted     string        `json:"formatted"`
}

// StorageRequest represents the request for storage operations
type StorageRequest struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

// StorageResponse represents the response from storage operations
type StorageResponse struct {
	Success bool   `json:"success"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// MowaError represents custom errors
type MowaError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
} 
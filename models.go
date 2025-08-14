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
// @Description Request to send messages to recipients
type MessageRequest struct {
	// @Description List of phone numbers or group names to send messages to
	// @Example ["+1234567890", "family", "+0987654321"]
	To []string `json:"to" binding:"required"`
	// @Description The message content to send
	// @Example "Hello from Mowa API!"
	Message string `json:"message" binding:"required"`
}

// MessageResponse represents the response from sending messages
// @Description Response containing results of message sending operations
type MessageResponse struct {
	// @Description List of results for each recipient
	Results []MessageResult `json:"results"`
}

// MessageResult represents the result of sending a message to one recipient
// @Description Result of sending a message to a single recipient
type MessageResult struct {
	// @Description The recipient phone number or group name
	// @Example "+1234567890"
	Recipient string `json:"recipient"`
	// @Description Whether the message was sent successfully
	Success bool `json:"success"`
	// @Description Error message if the message failed to send
	Error *string `json:"error,omitempty"`
}

// UptimeResponse represents the system uptime response
// @Description System uptime information
type UptimeResponse struct {
	// @Description Human-readable uptime string
	// @Example "2 days, 3 hours, 45 minutes"
	Uptime string `json:"uptime"`
	// @Description Uptime in seconds
	// @Example 176700
	UptimeSeconds float64 `json:"uptimeSeconds"`
	// @Description Formatted uptime string (same as uptime)
	// @Example "2 days, 3 hours, 45 minutes"
	Formatted string `json:"formatted"`
}

// StorageRequest represents the request for storage operations
// @Description Request for file storage operations
type StorageRequest struct {
	// @Description File path relative to storage directory
	// @Example "/documents/example.txt"
	Path string `json:"path"`
	// @Description File content (required for POST operations)
	// @Example "Hello, this is file content!"
	Content string `json:"content,omitempty"`
	// @Description List of phone numbers or group names to notify about the operation result
	// @Example ["some-group", "+1234567890"]
	Notify []string `json:"notify,omitempty"`
}

// StorageResponse represents the response from storage operations
// @Description Response from file storage operations
type StorageResponse struct {
	// @Description Whether the operation was successful
	Success bool `json:"success"`
	// @Description File content (for GET operations) or success message (for POST operations)
	Content string `json:"content,omitempty"`
	// @Description Error message if the operation failed
	Error string `json:"error,omitempty"`
}

// MowaError represents custom errors
// @Description Custom error response
type MowaError struct {
	// @Description Error message
	Message string `json:"message"`
	// @Description Error details
	Error string `json:"error"`
}

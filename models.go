package main



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

// MowaError represents custom errors
type MowaError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
} 
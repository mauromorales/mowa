package main

// Config represents the application configuration
type Config struct {
	Messages            MessagesConfig            `yaml:"messages"`
	Storage             StorageConfig             `yaml:"storage"`
	Reminders           RemindersConfig           `yaml:"reminders"`
	SoftwareUpdateCheck SoftwareUpdateCheckConfig `yaml:"software_update_check"`
}

// SoftwareUpdateCheckConfig configures the nightly `mowa check-updates` run
// that notifies recipients when a restart-required macOS update is available
// (issue #17: auto-installed OS updates de-register iMessage, so updates are
// installed manually and mowa reminds you when one is pending).
type SoftwareUpdateCheckConfig struct {
	// Enabled toggles the check. When omitted (nil) the check is enabled as
	// long as Notify has recipients, so configuring recipients is all that is
	// needed; an explicit `enabled: false` turns it off without deleting them.
	Enabled *bool `yaml:"enabled"`
	// Notify lists who to message about an available update. Entries are phone
	// numbers or group names, exactly like the `to`/`notify` fields of the
	// messaging endpoints (groups are expanded via messages.groups).
	Notify []string `yaml:"notify"`
	// Schedule is the local time of day ("HH:MM", 24h) the LaunchAgent runs
	// the check. Defaults to defaultUpdateCheckSchedule (03:00).
	Schedule string `yaml:"schedule"`
	// TimeoutSeconds bounds the `softwareupdate --list` call, which hits the
	// network and can take a minute or more. Defaults to
	// defaultUpdateCheckTimeoutSeconds.
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// isEnabled reports whether the update check should actually run: there must
// be someone to notify, and `enabled` must not be explicitly false.
func (c SoftwareUpdateCheckConfig) isEnabled() bool {
	return len(c.Notify) > 0 && (c.Enabled == nil || *c.Enabled)
}

// RemindersConfig represents the reminders configuration
type RemindersConfig struct {
	// TimeoutSeconds bounds how long a single Reminders osascript call may run
	// before it is killed and reported as an error. Reminders AppleScript can be
	// slow on large databases, so this defaults higher than the messages timeout
	// (defaultReminderTimeoutSeconds).
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

// MessagesConfig represents the messages configuration
type MessagesConfig struct {
	Groups map[string][]string `yaml:"groups"`
	// TimeoutSeconds bounds how long a single osascript send may run before
	// it is killed and reported as a failure. Defaults to defaultSendTimeoutSeconds.
	TimeoutSeconds int `yaml:"timeout_seconds"`
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

// UpdateRequest represents the request to self-update the running binary
// @Description Request to update mowa to a specific release, or the latest release when omitted
type UpdateRequest struct {
	// @Description Release version to install, with or without a leading "v" (e.g. "0.4.2" or "v0.4.2"). Omit to install the latest release.
	// @Example "0.4.2"
	Version string `json:"version,omitempty"`
}

// UpdateResponse represents the response from a self-update operation
// @Description Response from a self-update operation
type UpdateResponse struct {
	// @Description Whether the update was applied (false for errors and "already up to date")
	Success bool `json:"success"`
	// @Description The version that was running before the update
	// @Example "0.4.1"
	PreviousVersion string `json:"previousVersion,omitempty"`
	// @Description The version that is now installed on disk
	// @Example "0.4.2"
	InstalledVersion string `json:"installedVersion,omitempty"`
	// @Description Human-readable description of the result
	// @Example "Update installed; the service is restarting."
	Message string `json:"message"`
	// @Description Error message if the update failed
	Error string `json:"error,omitempty"`
}

// ReminderList represents a list in the macOS Reminders app
// @Description A Reminders list
type ReminderList struct {
	// @Description The list name
	// @Example "Groceries"
	Name string `json:"name"`
	// @Description Stable identifier for the list (use this to address it unambiguously)
	// @Example "x-apple-reminderkit://REMCDList/ABC123"
	ID string `json:"id"`
}

// ReminderListsResponse wraps the collection of Reminders lists
// @Description Response containing all Reminders lists
type ReminderListsResponse struct {
	// @Description The Reminders lists
	Lists []ReminderList `json:"lists"`
}

// Reminder represents a single reminder in the macOS Reminders app
// @Description A single reminder
type Reminder struct {
	// @Description Stable identifier for the reminder; address reminders by this id
	// @Example "x-apple-reminder://ABC123"
	ID string `json:"id"`
	// @Description The reminder title
	// @Example "Buy milk"
	Name string `json:"name"`
	// @Description Free-form notes attached to the reminder
	// @Example "Whole milk, 2 liters"
	Notes string `json:"notes"`
	// @Description Due date in RFC3339 format, or null if none is set
	// @Example "2026-07-20T09:00:00Z"
	DueDate *string `json:"due_date"`
	// @Description Whether the reminder is completed
	Completed bool `json:"completed"`
	// @Description Completion date in RFC3339 format, or null if not completed
	CompletionDate *string `json:"completion_date"`
	// @Description Name of the list that contains the reminder
	// @Example "Groceries"
	List string `json:"list"`
}

// RemindersResponse wraps the collection of reminders in a list
// @Description Response containing reminders in a list
type RemindersResponse struct {
	// @Description The reminders
	Reminders []Reminder `json:"reminders"`
}

// CreateListRequest is the body for creating a Reminders list
// @Description Request to create a new Reminders list
type CreateListRequest struct {
	// @Description The name for the new list
	// @Example "Groceries"
	Name string `json:"name" binding:"required"`
}

// CreateReminderRequest is the body for creating a reminder
// @Description Request to create a new reminder
type CreateReminderRequest struct {
	// @Description The target list, addressed by id or by name
	// @Example "Groceries"
	List string `json:"list" binding:"required"`
	// @Description The reminder title
	// @Example "Buy milk"
	Name string `json:"name" binding:"required"`
	// @Description Optional free-form notes
	// @Example "Whole milk, 2 liters"
	Notes string `json:"notes,omitempty"`
	// @Description Optional due date in RFC3339 format
	// @Example "2026-07-20T09:00:00Z"
	DueDate string `json:"due_date,omitempty"`
}

// UpdateReminderRequest is the body for editing a reminder. Every field is
// optional; only the fields present in the request are changed.
// @Description Request to update fields of an existing reminder (all fields optional)
type UpdateReminderRequest struct {
	// @Description New title
	// @Example "Buy oat milk"
	Name *string `json:"name,omitempty"`
	// @Description New notes
	// @Example "Barista edition"
	Notes *string `json:"notes,omitempty"`
	// @Description New due date in RFC3339 format
	// @Example "2026-07-21T09:00:00Z"
	DueDate *string `json:"due_date,omitempty"`
	// @Description Mark complete (true) or incomplete (false)
	Completed *bool `json:"completed,omitempty"`
	// @Description Move the reminder to another list. Not supported by the macOS
	// Reminders scripting interface; supplying this returns 501 Not Implemented.
	List *string `json:"list,omitempty"`
}

// ReminderErrorResponse is the error body returned by reminders endpoints
// @Description Error response from a reminders operation
type ReminderErrorResponse struct {
	// @Description Human-readable error message
	// @Example "list not found"
	Error string `json:"error"`
}

// MowaError represents custom errors
// @Description Custom error response
type MowaError struct {
	// @Description Error message
	Message string `json:"message"`
	// @Description Error details
	Error string `json:"error"`
}

package model

import "time"

// Flow represents a complete bot conversation flow (e.g., "main", "restore").
type Flow struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Stages    []Stage   `json:"stages"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Stage represents a single step in a conversation flow.
type Stage struct {
	ID        string   `json:"id"`
	Message   string   `json:"message"`
	Buttons   []Button `json:"buttons,omitempty"`
	Input     string   `json:"input,omitempty"`      // "text", "photo", "reaction", "none", ""
	OnSuccess string   `json:"on_success,omitempty"` // next stage ID on success
	OnFailure string   `json:"on_failure,omitempty"` // next stage ID on failure
	Action    string   `json:"action,omitempty"`     // named action: "call_ministry", "req_vip", "put_receipt", etc.
}

// Button represents an inline keyboard button shown to the user.
type Button struct {
	Label  string `json:"label"`
	Action string `json:"action"` // "goto" (go to stage), "call" (execute action), "url" (open link)
	Target string `json:"target"` // stage ID, action name, or URL
}

// FlowUpdate is the payload for updating a flow via the API.
type FlowUpdate struct {
	Name   string  `json:"name,omitempty"`
	Stages []Stage `json:"stages"`
}

// FlowListItem is a summary returned when listing all flows.
type FlowListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	StageCount int       `json:"stage_count"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DecisionComment represents a single decision template used when reviewing receipts.
type DecisionComment struct {
	Code           int    `json:"code"`
	Key            string `json:"key"`
	Button         string `json:"button"`
	Template       string `json:"template"`
	HasSupportLink bool   `json:"has_support_link"`
}

// DecisionConfig is the top-level structure for the decisions JSON file.
type DecisionConfig struct {
	Decisions           []DecisionComment `json:"decisions"`
	SupportLinkTemplate string            `json:"support_link_template"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// DecisionUpdate is the payload for updating decisions via the API.
type DecisionUpdate struct {
	Decisions           []DecisionComment `json:"decisions"`
	SupportLinkTemplate string            `json:"support_link_template,omitempty"`
}

// MinistryMessages holds all translatable messages used in ministry.go.
type MinistryMessages struct {
	Messages  map[string]string `json:"messages"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// MinistryMessagesUpdate is the payload for updating ministry messages.
type MinistryMessagesUpdate struct {
	Messages map[string]string `json:"messages"`
}

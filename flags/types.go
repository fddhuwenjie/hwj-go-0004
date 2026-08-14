package flags

import "time"

// Condition matches one context attribute against one or more values.
type Condition struct {
	Attribute string   `json:"attribute"`
	Operator  string   `json:"operator"`
	Values    []string `json:"values"`
}

// Rule returns Value when all Conditions match. Lower priorities run first.
type Rule struct {
	ID         string      `json:"id"`
	Priority   int         `json:"priority"`
	Conditions []Condition `json:"conditions"`
	Value      string      `json:"value"`
}

// RolloutOption assigns Weight basis points out of 10,000 to Value.
type RolloutOption struct {
	Value  string `json:"value"`
	Weight int    `json:"weight"`
}

// Prerequisite requires another flag to evaluate to Value.
type Prerequisite struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Schedule is a local wall-clock interval in an IANA timezone. End is exclusive.
type Schedule struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Location string `json:"location"`
}

// Flag is a complete versioned feature-flag definition.
type Flag struct {
	Key           string          `json:"key"`
	Enabled       bool            `json:"enabled"`
	Default       string          `json:"default"`
	Rules         []Rule          `json:"rules,omitempty"`
	Rollout       []RolloutOption `json:"rollout,omitempty"`
	Prerequisites []Prerequisite  `json:"prerequisites,omitempty"`
	Schedule      *Schedule       `json:"schedule,omitempty"`
	Version       int64           `json:"version"`
}

// Context supplies a stable subject key and targeting attributes.
type Context struct {
	SubjectKey string            `json:"subjectKey"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Evaluation is the public result of one flag evaluation.
type Evaluation struct {
	FlagKey     string    `json:"flagKey"`
	Value       string    `json:"value"`
	Reason      string    `json:"reason"`
	Version     int64     `json:"version"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

// AuditEvent records one completed public evaluation.
type AuditEvent struct {
	Sequence   int64     `json:"sequence"`
	FlagKey    string    `json:"flagKey"`
	SubjectKey string    `json:"subjectKey"`
	Value      string    `json:"value"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}

// AuditQuery filters evaluation history. Zero times are unbounded.
type AuditQuery struct {
	FlagKey    string    `json:"flagKey,omitempty"`
	SubjectKey string    `json:"subjectKey,omitempty"`
	From       time.Time `json:"from,omitempty"`
	To         time.Time `json:"to,omitempty"`
	Limit      int       `json:"limit,omitempty"`
}

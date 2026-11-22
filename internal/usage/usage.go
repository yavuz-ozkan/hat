// Package usage shows each account's plan limits.
//
// By default the numbers come from what Claude Code hands its status line,
// which "hat statusline" records per account (recorded.go). That data is
// documented and needs no token. "hat usage --live" instead asks the endpoint
// behind Claude Code's own /usage screen (live.go), which isn't documented.
package usage

import (
	"time"

	"github.com/yavuz-ozkan/hat/internal/account"
)

// Problem codes, stable for anything that reads --json.
const (
	NotSignedIn        = "not_signed_in"
	SignInExpired      = "sign_in_expired"
	SignInRejected     = "sign_in_rejected"
	RateLimited        = "rate_limited"
	Unreachable        = "unreachable"
	UnexpectedResponse = "unexpected_response"
	NoUsageRecorded    = "no_usage_recorded"
)

type Limit struct {
	Kind     string     `json:"kind"`
	Label    string     `json:"label"`
	Model    string     `json:"model,omitempty"`
	Percent  float64    `json:"percent"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Row struct {
	Account string  `json:"account"`
	Kind    string  `json:"kind"`
	Email   string  `json:"email"`
	Limits  []Limit `json:"limits"`
	// AsOf is when the limits were read: when the status line last recorded
	// them, or when --live asked Anthropic.
	AsOf    *time.Time `json:"asOf,omitempty"`
	Problem *Problem   `json:"problem,omitempty"`
}

var kindNames = map[account.Kind]string{account.Main: "main", account.Managed: "managed", account.Unmanaged: "unmanaged"}

package datalab

import (
	"time"
)

const (
	// DeviceAuthorizationLifetime is the maximum time an unattended agent may
	// wait for its human to approve it.
	DeviceAuthorizationLifetime = 10 * time.Minute
	// DeviceAuthorizationPollInterval is the initial RFC-8628-style polling
	// delay. The server can increase it after an early poll.
	DeviceAuthorizationPollInterval = 5 * time.Second
	// DeviceTokenMaxLifetime bounds unattended credentials independently of a
	// browser session's lifetime.
	DeviceTokenMaxLifetime = 30 * 24 * time.Hour
)

type DeviceAuthorizationState string

const (
	DeviceAuthorizationPending  DeviceAuthorizationState = "pending"
	DeviceAuthorizationApproved DeviceAuthorizationState = "approved"
	DeviceAuthorizationDenied   DeviceAuthorizationState = "denied"
	DeviceAuthorizationConsumed DeviceAuthorizationState = "consumed"
	DeviceAuthorizationExpired  DeviceAuthorizationState = "expired"
)

// DeviceAuthorization is durable pairing state. It intentionally never carries
// the raw device code, displayed user code, or raw API token.
type DeviceAuthorization struct {
	ID              string  `json:"id"`
	RequestedName   string  `json:"requested_name"`
	RequestedScopes []Scope `json:"requested_scopes"`
	// ExpiresAt bounds the pairing ceremony. TokenLifetime is the requested
	// credential duration; TokenExpiresAt is internal state set to its actual
	// deadline only when the approved authorization is consumed.
	ExpiresAt            time.Time                `json:"expires_at"`
	TokenExpiresAt       time.Time                `json:"-"`
	TokenLifetime        time.Duration            `json:"-"`
	TokenLifetimeSeconds int64                    `json:"token_lifetime_seconds"`
	PollInterval         time.Duration            `json:"poll_interval"`
	NextPollAt           time.Time                `json:"next_poll_at"`
	State                DeviceAuthorizationState `json:"state"`
	ApprovedBy           string                   `json:"approved_by,omitempty"`
	ApprovedAt           *time.Time               `json:"approved_at,omitempty"`
	DeniedAt             *time.Time               `json:"denied_at,omitempty"`
	ConsumedAt           *time.Time               `json:"consumed_at,omitempty"`
	TokenID              string                   `json:"token_id,omitempty"`
	CreatedAt            time.Time                `json:"created_at"`
}

type StartDeviceAuthorizationRequest struct {
	Name      string  `json:"name"`
	Scopes    []Scope `json:"scopes"`
	ExpiresIn string  `json:"expires_in"`
}

type StartDeviceAuthorizationResponse struct {
	AuthorizationID         string `json:"authorization_id"`
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type PollDeviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
}

type ApproveDeviceAuthorizationRequest struct {
	UserCode string `json:"user_code"`
}

type DeviceTokenResponse struct {
	Token     string     `json:"token"`
	TokenID   string     `json:"token_id"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Scopes    []Scope    `json:"scopes"`
}

const (
	ActionDeviceAuthorizationStart   = "device_authorization.start"
	ActionDeviceAuthorizationApprove = "device_authorization.approve"
	ActionDeviceAuthorizationDeny    = "device_authorization.deny"
	ActionDeviceAuthorizationConsume = "device_authorization.consume"
)

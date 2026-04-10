package cli

import "time"

// Flag names.
const (
	FlagEndpoint       = "endpoint"
	FlagRegion         = "region"
	FlagUser           = "user"
	FlagSchema         = "schema"
	FlagProfile        = "profile"
	FlagRoleARN        = "role-arn"
	FlagConnectTimeout = "connect-timeout"
	FlagAllowHazards   = "allow-hazards"
	FlagForce          = "force"
	FlagListen         = "listen"
	FlagUpstream       = "upstream"
	FlagRetries        = "retries"
	FlagRetryDelay     = "retry-delay"
)

// Defaults.
const (
	DefaultUser           = "admin"
	DefaultConnectTimeout = 10 * time.Second
	DefaultListen         = ":15432"
	DefaultUpstream       = "localhost:5432"
	DefaultRetries        = 5
	DefaultRetryDelay     = 2 * time.Second
)

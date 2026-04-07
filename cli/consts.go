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
)

// Defaults.
const (
	DefaultUser           = "admin"
	DefaultConnectTimeout = 10 * time.Second
)

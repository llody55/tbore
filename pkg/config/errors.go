package config

import "errors"

var (
	ErrTokenRequired       = errors.New("token is required")
	ErrInvalidPortRange    = errors.New("invalid port range")
	ErrInvalidMaxConnections = errors.New("max_connections must be greater than 0")
	ErrInvalidMaxTunnels   = errors.New("max_tunnels_per_client must be greater than 0")
)
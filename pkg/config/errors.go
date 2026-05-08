package config

import "errors"

var (
	ErrTokenRequired           = errors.New("token is required")
	ErrInvalidPortRange        = errors.New("invalid port range")
	ErrInvalidMaxConnections   = errors.New("max_connections must be greater than 0")
	ErrInvalidMaxTunnels       = errors.New("max_tunnels_per_client must be greater than 0")
	ErrServerAddrRequired      = errors.New("server_addr is required")
	ErrServerPortRequired      = errors.New("server_port is required")
	ErrTunnelNameRequired      = errors.New("tunnel name is required")
	ErrTunnelLocalIPRequired   = errors.New("tunnel local_ip is required")
	ErrTunnelLocalPortRequired = errors.New("tunnel local_port is required")
)

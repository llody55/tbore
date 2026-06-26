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

	// 认证失败限制参数校验错误
	ErrInvalidAuthMaxFailures   = errors.New("auth_max_failures must be greater than 0")
	ErrInvalidAuthBlockDuration = errors.New("auth_block_duration must be greater than 0")
	ErrInvalidAuthLRUSize       = errors.New("auth_lru_size must be greater than 0")
	ErrInvalidAuthCleanInterval = errors.New("auth_clean_interval must be greater than 0")
	ErrInvalidAuthRecordTTL     = errors.New("auth_record_ttl must be greater than 0")
)

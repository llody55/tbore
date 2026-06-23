package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultMinPort             = 1024
	DefaultMaxPort             = 65535
	DefaultMaxConns            = 100
	DefaultMaxTunnels          = 10
	DefaultControlPort         = 7835
	DefaultHealthCheckInterval = 30
	// DefaultMaxConnsPerTunnel 为 0 表示跟随 MaxConnections，不单独限制
	DefaultMaxConnsPerTunnel = 0
)

type TunnelConfig struct {
	Name       string `yaml:"name"`
	LocalIP    string `yaml:"local_ip"`
	LocalPort  int    `yaml:"local_port"`
	RemotePort uint32 `yaml:"remote_port"`
	Timeout    int    `yaml:"timeout"`
}

type ServerConfig struct {
	Port              int    `yaml:"port"`
	Token             string `yaml:"token"`
	MinPort           uint32 `yaml:"min_port"`
	MaxPort           uint32 `yaml:"max_port"`
	MaxConnections    int    `yaml:"max_connections"`
	MaxTunnels        int    `yaml:"max_tunnels_per_client"`
	BindAddr          string `yaml:"bind_addr"`
	HostKeyPath       string `yaml:"host_key_path"`
	MaxConnsPerTunnel int    `yaml:"max_conns_per_tunnel"`
}

type ClientConfig struct {
	ServerAddr          string         `yaml:"server_addr"`
	ServerPort          int            `yaml:"server_port"`
	Token               string         `yaml:"token"`
	HostKey             string         `yaml:"host_key"`
	Project             string         `yaml:"project"`
	Region              string         `yaml:"region"`
	Tunnels             []TunnelConfig `yaml:"tunnels"`
	HealthCheckInterval int            `yaml:"health_check_interval"`
}

func LoadServerConfig(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ServerConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	cfg.setDefaults()
	return &cfg, nil
}

func LoadClientConfig(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ClientConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	cfg.setDefaults()
	return &cfg, nil
}

func (cfg *ClientConfig) setDefaults() {
	if cfg.HealthCheckInterval <= 0 {
		cfg.HealthCheckInterval = DefaultHealthCheckInterval
	}
}

func (cfg *ServerConfig) setDefaults() {
	if cfg.Port == 0 {
		cfg.Port = DefaultControlPort
	}
	if cfg.MinPort == 0 {
		cfg.MinPort = DefaultMinPort
	}
	if cfg.MaxPort == 0 {
		cfg.MaxPort = DefaultMaxPort
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = DefaultMaxConns
	}
	if cfg.MaxTunnels == 0 {
		cfg.MaxTunnels = DefaultMaxTunnels
	}
	if cfg.MaxConnsPerTunnel == 0 {
		// 未显式配置时，与全局 MaxConnections 保持一致，防止单条隧道独占全部 FD
		cfg.MaxConnsPerTunnel = cfg.MaxConnections
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = "0.0.0.0"
	}
}

func (cfg *ServerConfig) Validate() error {
	if cfg.Token == "" {
		return ErrTokenRequired
	}
	if cfg.MinPort > cfg.MaxPort {
		return ErrInvalidPortRange
	}
	if cfg.MaxConnections <= 0 {
		return ErrInvalidMaxConnections
	}
	if cfg.MaxTunnels <= 0 {
		return ErrInvalidMaxTunnels
	}
	return nil
}

func (cfg *ServerConfig) IsValidPort(port uint32) bool {
	if port == 0 {
		return true
	}
	return port >= cfg.MinPort && port <= cfg.MaxPort
}

func (cfg *ClientConfig) Validate() error {
	if cfg.ServerAddr == "" {
		return ErrServerAddrRequired
	}
	if cfg.ServerPort == 0 {
		return ErrServerPortRequired
	}
	if cfg.Token == "" {
		return ErrTokenRequired
	}
	for i, tunnel := range cfg.Tunnels {
		if tunnel.Name == "" {
			return fmt.Errorf("tunnel[%d]: %w", i, ErrTunnelNameRequired)
		}
		if tunnel.LocalIP == "" {
			return fmt.Errorf("tunnel[%d] (%s): %w", i, tunnel.Name, ErrTunnelLocalIPRequired)
		}
		if tunnel.LocalPort == 0 {
			return fmt.Errorf("tunnel[%d] (%s): %w", i, tunnel.Name, ErrTunnelLocalPortRequired)
		}
	}
	return nil
}

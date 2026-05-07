package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultMinPort     = 1024
	DefaultMaxPort     = 65535
	DefaultMaxConns    = 100
	DefaultMaxTunnels  = 10
	DefaultControlPort = 7835
)

type TunnelConfig struct {
	Name       string `yaml:"name"`
	LocalIP    string `yaml:"local_ip"`
	LocalPort  int    `yaml:"local_port"`
	RemotePort uint32 `yaml:"remote_port"`
}

type ServerConfig struct {
	Port             int    `yaml:"port"`
	Token            string `yaml:"token"`
	MinPort          uint32 `yaml:"min_port"`
	MaxPort          uint32 `yaml:"max_port"`
	MaxConnections   int    `yaml:"max_connections"`
	MaxTunnels       int    `yaml:"max_tunnels_per_client"`
	BindAddr         string `yaml:"bind_addr"`
	HostKeyPath      string `yaml:"host_key_path"`
}

type ClientConfig struct {
	ServerAddr string         `yaml:"server_addr"`
	ServerPort int            `yaml:"server_port"`
	Token      string         `yaml:"token"`
	HostKey    string         `yaml:"host_key"`
	Tunnels    []TunnelConfig `yaml:"tunnels"`
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

	return &cfg, nil
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
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

	// 认证失败限制相关默认值
	DefaultAuthMaxFailures   = 3              // 连续失败 N 次后拉黑
	DefaultAuthBlockDuration = 600            // 拉黑时长（秒），10 分钟
	DefaultAuthLRUSize       = 1000           // 内存 LRU 缓存大小（条）
	DefaultAuthDBPath        = "data/auth.db" // 相对工作目录；推荐配置为 /var/lib/tbore/auth.db
	DefaultAuthCleanInterval = 60             // 后台清理周期（秒）
	DefaultAuthRecordTTL     = 86400          // 过期/历史记录磁盘保留时长（秒），24 小时
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

	// 认证失败限制（爆破防护）
	// AuthDisabled 控制是否禁用认证失败限制。零值 false 表示启用（默认安全），
	// 显式设为 true 时完全禁用，回到旧行为（不做失败计数/拉黑）。仅排障/兼容场景用。
	// 采用 "disabled" 而非 "enabled" 是为了规避 Go bool 零值陷阱，实现零配置即安全。
	AuthDisabled      bool   `yaml:"auth_disabled"`
	AuthMaxFailures   int    `yaml:"auth_max_failures"`   // 连续失败 N 次后拉黑，默认 3
	AuthBlockDuration int    `yaml:"auth_block_duration"` // 拉黑时长（秒），默认 600
	AuthLRUSize       int    `yaml:"auth_lru_size"`       // 内存 LRU 缓存大小，默认 1000
	AuthBlockDBPath   string `yaml:"auth_block_db_path"`  // bbolt 文件路径，留空走默认
	AuthCleanInterval int    `yaml:"auth_clean_interval"` // 后台清理周期（秒），默认 60
	AuthRecordTTL     int    `yaml:"auth_record_ttl"`     // 过期记录磁盘保留时长（秒），默认 86400
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

	// 认证失败限制默认值（AuthDisabled 零值 false 表示启用，无需特殊处理）
	if cfg.AuthMaxFailures == 0 {
		cfg.AuthMaxFailures = DefaultAuthMaxFailures
	}
	if cfg.AuthBlockDuration == 0 {
		cfg.AuthBlockDuration = DefaultAuthBlockDuration
	}
	if cfg.AuthLRUSize == 0 {
		cfg.AuthLRUSize = DefaultAuthLRUSize
	}
	if cfg.AuthCleanInterval == 0 {
		cfg.AuthCleanInterval = DefaultAuthCleanInterval
	}
	if cfg.AuthRecordTTL == 0 {
		cfg.AuthRecordTTL = DefaultAuthRecordTTL
	}
	// DBPath 为空时不在此处填默认：交由 server 端在启用时填入默认路径，
	// 以保证"零配置即安全"，同时允许用户通过 AuthDisabled 显式关闭。
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

	// 认证失败限制参数校验（仅在启用时校验，禁用时不强制）
	if !cfg.AuthDisabled {
		if cfg.AuthMaxFailures <= 0 {
			return ErrInvalidAuthMaxFailures
		}
		if cfg.AuthBlockDuration <= 0 {
			return ErrInvalidAuthBlockDuration
		}
		if cfg.AuthLRUSize <= 0 {
			return ErrInvalidAuthLRUSize
		}
		if cfg.AuthCleanInterval <= 0 {
			return ErrInvalidAuthCleanInterval
		}
		if cfg.AuthRecordTTL <= 0 {
			return ErrInvalidAuthRecordTTL
		}
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

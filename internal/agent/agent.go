package agent

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// Config Agent配置
type Config struct {
	ServerAddr string
	Token      string
	AgentID    string
	Version    string
}

// TunnelConfig 隧道配置
type TunnelConfig struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	Status     string `json:"status"`
}

// SSHConnection SSH连接信息
type SSHConnection struct {
	Client    *ssh.Client
	Listeners map[uint]net.Listener
	Mutex     sync.Mutex
}

// SSHServerConfig SSH服务器配置
type SSHServerConfig struct {
	Host     string
	Port     int
	Username string
	Password string
}

// Agent 结构体
type Agent struct {
	config             Config
	isRunning          bool
	heartbeatTicker    *time.Ticker
	configFile         string
	tunnels            map[uint]*TunnelConfig
	tunnelUpdateTicker *time.Ticker
	sshConn            *SSHConnection
	sshConfig          SSHServerConfig
	sshMutex           sync.Mutex
	sshReconnectTicker *time.Ticker
	tunnelStatusTicker *time.Ticker

	// WebSocket相关字段
	wsConn            *websocket.Conn
	wsMutex           sync.Mutex
	wsReconnectTicker *time.Ticker
}

// DiskMount 磁盘挂载点信息
type DiskMount struct {
	MountPoint string `json:"mount_point"`
	Total      uint64 `json:"total"`
	Used       uint64 `json:"used"`
	Free       uint64 `json:"free"`
	Fstype     string `json:"fstype"`
}

// SystemInfo 系统信息
type SystemInfo struct {
	CPUModel    string  `json:"cpu_model"`
	CPUCores    int     `json:"cpu_cores"`
	MemoryTotal uint64  `json:"memory_total"`
	MemoryUsed  uint64  `json:"memory_used"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapUsed    uint64  `json:"swap_used"`
	Load1       float64 `json:"load_1"`
	Load5       float64 `json:"load_5"`
	Load15      float64 `json:"load_15"`
	Uptime      uint64  `json:"uptime"`
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	AgentID    string      `json:"agent_id"`
	IPAddress  string      `json:"ip_address"`
	OS         string      `json:"os"`
	Version    string      `json:"version"`
	SystemInfo SystemInfo  `json:"system_info"`
	DiskMounts []DiskMount `json:"disk_mounts"`
	Token      string      `json:"token"`
}

// HeartbeatRequest 心跳请求
type HeartbeatRequest struct {
	AgentID string `json:"agent_id"`
}

const (
	// AgentVersion Agent版本号
	AgentVersion = "v0.1.0"
	// DefaultConfigFile 默认配置文件路径
	DefaultConfigFile = "agent.yaml"
)

// NewAgent 创建Agent实例
func NewAgent(config Config) *Agent {
	return &Agent{
		config:             config,
		isRunning:          false,
		heartbeatTicker:    nil,
		tunnels:            make(map[uint]*TunnelConfig),
		tunnelUpdateTicker: nil,
		sshConn:            nil,
		sshConfig:          SSHServerConfig{}, // 清空SSH配置，不再尝试SSH连接
		sshMutex:           sync.Mutex{},
		sshReconnectTicker: nil,
		tunnelStatusTicker: nil,
	}
}

// Start 启动Agent
func (a *Agent) Start() {
	log.Printf("Starting tbore agent...")

	// 注册设备
	if err := a.register(); err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}

	// 启动WebSocket连接
	if err := a.connectWebSocket(); err != nil {
		log.Printf("Failed to connect WebSocket: %v", err)
		// 启动WebSocket重连机制
		a.startWebSocketReconnect()
	}

	// 启动心跳
	a.startHeartbeat()

	// 启动隧道配置更新
	a.startTunnelConfigUpdate()

	// 启动隧道状态更新
	a.startTunnelStatusUpdate()

	// 立即获取一次隧道配置
	if err := a.updateTunnelConfigs(); err != nil {
		log.Printf("Failed to update tunnel configs: %v", err)
	} else {
		// 同步隧道
		a.syncTunnels()
	}

	a.isRunning = true
	log.Printf("Agent started successfully")
}

package model

import (
	"time"
)

// Token 模型
type Token struct {
	ID          uint      `gorm:"primary_key" json:"id"`
	Name        string    `gorm:"size:50;not null;unique" json:"name"`
	Value       string    `gorm:"size:255;not null;unique" json:"value"`
	Description string    `gorm:"size:255" json:"description"`
	Status      string    `gorm:"size:20;not null;default:'active'" json:"status"`
	CreatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// NetworkInfo 网络信息模型
type NetworkInfo struct {
	ID          uint      `gorm:"primary_key" json:"id"`
	DeviceID    uint      `gorm:"not null" json:"device_id"`
	Interface   string    `gorm:"size:50" json:"interface"`
	IPAddress   string    `gorm:"size:50" json:"ip_address"`
	MACAddress  string    `gorm:"size:20" json:"mac_address"`
	BytesSent   uint64    `json:"bytes_sent"`
	BytesRecv   uint64    `json:"bytes_recv"`
	PacketsSent uint64    `json:"packets_sent"`
	PacketsRecv uint64    `json:"packets_recv"`
	CreatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// DiskMount 磁盘挂载点模型
type DiskMount struct {
	ID         uint      `gorm:"primary_key" json:"id"`
	DeviceID   uint      `gorm:"not null" json:"device_id"`
	MountPoint string    `gorm:"size:100;not null" json:"mount_point"`
	Total      uint64    `json:"total"`
	Used       uint64    `json:"used"`
	Free       uint64    `json:"free"`
	Fstype     string    `gorm:"size:20" json:"fstype"`
	CreatedAt  time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt  time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// SystemInfo 系统信息模型
type SystemInfo struct {
	ID          uint      `gorm:"primary_key" json:"id"`
	DeviceID    uint      `gorm:"not null;unique" json:"device_id"`
	CPUModel    string    `gorm:"size:100" json:"cpu_model"`
	CPUCores    int       `json:"cpu_cores"`
	MemoryTotal uint64    `json:"memory_total"`
	MemoryUsed  uint64    `json:"memory_used"`
	Load1       float64   `json:"load_1"`
	Load5       float64   `json:"load_5"`
	Load15      float64   `json:"load_15"`
	Uptime      uint64    `json:"uptime"`     // 系统运行时间
	SwapTotal   uint64    `json:"swap_total"` // 交换分区总量
	SwapUsed    uint64    `json:"swap_used"`  // 交换分区使用量
	CreatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// Tag 标签模型
type Tag struct {
	ID          uint      `gorm:"primary_key" json:"id"`
	Name        string    `gorm:"size:50;not null;unique" json:"name"`
	Description string    `gorm:"size:255" json:"description"`
	CreatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Devices     []Device  `gorm:"many2many:device_tags;" json:"devices,omitempty"`
}

// DeviceTag 设备标签关联表
type DeviceTag struct {
	DeviceID uint `gorm:"primary_key"`
	TagID    uint `gorm:"primary_key"`
}

// Device 设备模型
type Device struct {
	ID            uint          `gorm:"primary_key" json:"id"`
	Name          string        `gorm:"size:100;not null" json:"name"`
	AgentID       string        `gorm:"size:64;not null;unique" json:"agent_id"`
	IPAddress     string        `gorm:"size:50;not null" json:"ip_address"`
	OS            string        `gorm:"size:50;not null" json:"os"`
	Version       string        `gorm:"size:20" json:"version"`
	TokenID       uint          `gorm:"not null" json:"token_id"`
	Status        string        `gorm:"size:20;not null;default:'offline'" json:"status"`
	LastHeartbeat time.Time     `gorm:"not null;default:CURRENT_TIMESTAMP" json:"last_heartbeat"`
	CreatedAt     time.Time     `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time     `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	SystemInfo    *SystemInfo   `gorm:"foreignkey:DeviceID" json:"system_info,omitempty"`
	NetworkInfos  []NetworkInfo `gorm:"foreignkey:DeviceID" json:"network_infos,omitempty"`
	DiskMounts    []DiskMount   `gorm:"foreignkey:DeviceID" json:"disk_mounts,omitempty"`
	Tags          []Tag         `gorm:"many2many:device_tags;" json:"tags,omitempty"`
	Tunnels       []Tunnel      `gorm:"foreignkey:DeviceID" json:"tunnels,omitempty"`
	Token         Token         `gorm:"foreignkey:TokenID" json:"token,omitempty"`
}

// Tunnel 隧道模型
type Tunnel struct {
	ID               uint        `gorm:"primary_key" json:"id"`
	DeviceID         uint        `gorm:"not null" json:"device_id"`
	Name             string      `gorm:"size:100;not null" json:"name"`
	Type             string      `gorm:"size:20;not null;default:'tcp'" json:"type"`
	LocalIP          string      `gorm:"size:50;not null" json:"local_ip"`
	LocalPort        int         `gorm:"not null" json:"local_port"`
	RemotePort       int         `gorm:"not null" json:"remote_port"`
	Status           string      `gorm:"size:20;not null;default:'inactive'" json:"status"`
	ConnectionCount  int64       `gorm:"default:0" json:"connection_count"`  // 当前连接数
	TotalConnections int64       `gorm:"default:0" json:"total_connections"` // 总连接数
	BytesSent        int64       `gorm:"default:0" json:"bytes_sent"`        // 发送字节数
	BytesRecv        int64       `gorm:"default:0" json:"bytes_recv"`        // 接收字节数
	LastActive       time.Time   `gorm:"" json:"last_active"`
	CreatedAt        time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
	Logs             []TunnelLog `gorm:"foreignkey:TunnelID" json:"logs,omitempty"`
}

// TunnelLog 隧道日志模型
type TunnelLog struct {
	ID        uint      `gorm:"primary_key" json:"id"`
	TunnelID  uint      `gorm:"not null" json:"tunnel_id"`
	Level     string    `gorm:"size:20;not null" json:"level"` // debug, info, warning, error
	Message   string    `gorm:"size:500;not null" json:"message"`
	Source    string    `gorm:"size:100" json:"source"` // 日志来源
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// User 用户模型
type User struct {
	ID        uint      `gorm:"primary_key"`
	Username  string    `gorm:"size:50;not null;unique"`
	Password  string    `gorm:"size:255;not null"`
	Role      string    `gorm:"size:20;not null;default:'user'"`
	CreatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
}

// AccountDevice 账号设备关联表
type AccountDevice struct {
	AccountID uint `gorm:"primary_key" json:"account_id"`
	DeviceID  uint `gorm:"primary_key" json:"device_id"`
}

// Account 账号模型
type Account struct {
	ID           uint      `gorm:"primary_key" json:"id"`
	Name         string    `gorm:"size:100;not null;unique" json:"name"`
	Username     string    `gorm:"size:100;not null" json:"username"`
	AuthType     string    `gorm:"size:20;not null;default:'password'" json:"auth_type"` // 认证类型：password, key等
	Password     string    `gorm:"size:255;not null" json:"password"`
	Description  string    `gorm:"size:500" json:"description"`
	IsActive     bool      `gorm:"not null;default:true" json:"is_active"`      // 是否激活
	IsPrivileged bool      `gorm:"not null;default:false" json:"is_privileged"` // 是否是特权账户
	CreatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	Devices      []Device  `gorm:"many2many:account_devices;" json:"devices,omitempty"` // 关联的多个设备
}

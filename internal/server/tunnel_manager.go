package server

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// TunnelManager 隧道管理器
type TunnelManager struct {
	mu            sync.Mutex
	tunnels       map[uint]*Tunnel
	tunnelService *TunnelService
}

// Tunnel 隧道实例
type Tunnel struct {
	ID               uint
	DeviceID         uint
	Name             string
	Type             string
	LocalIP          string
	LocalPort        int
	RemotePort       int
	Status           string // active, inactive, error, connecting
	Listener         net.Listener
	UDPConn          *net.UDPConn
	UDPSessions      map[string]*net.UDPAddr // UDP会话映射，key为源地址:端口
	ConnectionCount  int64                   // 当前连接数
	TotalConnections int64                   // 总连接数
	BytesSent        int64                   // 发送字节数
	BytesRecv        int64                   // 接收字节数
	LastActive       time.Time               // 最后活跃时间
	Error            string                  // 错误信息
	mutex            sync.Mutex              // 用于保护统计信息
	udpMutex         sync.Mutex              // 用于保护UDP会话映射
}

// NewTunnelManager 创建新的隧道管理器
func NewTunnelManager(tunnelService *TunnelService) *TunnelManager {
	return &TunnelManager{
		tunnels:       make(map[uint]*Tunnel),
		tunnelService: tunnelService,
	}
}

// StartTunnel 启动隧道
func (tm *TunnelManager) StartTunnel(tunnelID uint, deviceID uint, name, tunnelType, localIP string, localPort, remotePort int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 检查隧道是否已存在
	if _, exists := tm.tunnels[tunnelID]; exists {
		return fmt.Errorf("tunnel %d already exists", tunnelID)
	}

	// 创建隧道实例
	tunnel := &Tunnel{
		ID:               tunnelID,
		DeviceID:         deviceID,
		Name:             name,
		Type:             tunnelType,
		LocalIP:          localIP,
		LocalPort:        localPort,
		RemotePort:       remotePort,
		Status:           "active",
		ConnectionCount:  0,
		TotalConnections: 0,
		BytesSent:        0,
		BytesRecv:        0,
		LastActive:       time.Now(),
		Error:            "",
	}

	// 根据隧道类型创建监听器
	if tunnelType == "udp" {
		// 创建UDP监听器
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", remotePort))
		if err != nil {
			msg := fmt.Sprintf("Failed to resolve UDP address for port %d: %v", remotePort, err)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnelID, "error", msg, "StartTunnel")
			}
			return fmt.Errorf(msg)
		}

		udpConn, err := net.ListenUDP("udp", addr)
		if err != nil {
			msg := fmt.Sprintf("Failed to listen on UDP port %d: %v", remotePort, err)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnelID, "error", msg, "StartTunnel")
			}
			return fmt.Errorf(msg)
		}

		tunnel.UDPConn = udpConn
		tunnel.UDPSessions = make(map[string]*net.UDPAddr)

		// 保存隧道
		tm.tunnels[tunnelID] = tunnel

		// 启动UDP监听协程
		go tm.acceptUDPConnections(tunnel)
	} else {
		// 创建TCP监听器
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", remotePort))
		if err != nil {
			msg := fmt.Sprintf("Failed to listen on TCP port %d: %v", remotePort, err)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnelID, "error", msg, "StartTunnel")
			}
			return fmt.Errorf(msg)
		}

		tunnel.Listener = listener

		// 保存隧道
		tm.tunnels[tunnelID] = tunnel

		// 启动TCP监听协程
		go tm.acceptConnections(tunnel)
	}

	msg := fmt.Sprintf("Tunnel started: %s:%d -> %s:%d (remote port %d)",
		localIP, localPort, tunnelType, remotePort)
	log.Printf("Tunnel %d %s", tunnelID, msg)

	if tm.tunnelService != nil {
		tm.tunnelService.AddTunnelLog(tunnelID, "info", msg, "StartTunnel")
	}

	return nil
}

// StopTunnel 停止隧道
func (tm *TunnelManager) StopTunnel(tunnelID uint) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 检查隧道是否存在
	tunnel, exists := tm.tunnels[tunnelID]
	if !exists {
		return fmt.Errorf("tunnel %d does not exist", tunnelID)
	}

	// 根据隧道类型关闭不同的监听器
	var err error
	if tunnel.Type == "udp" {
		// 关闭UDP连接
		if err = tunnel.UDPConn.Close(); err != nil {
			msg := fmt.Errorf("failed to close UDP connection for tunnel %d: %v", tunnelID, err)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnelID, "error", msg.Error(), "StopTunnel")
			}
			return msg
		}
	} else {
		// 关闭TCP监听器
		if err = tunnel.Listener.Close(); err != nil {
			msg := fmt.Errorf("failed to close TCP listener for tunnel %d: %v", tunnelID, err)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnelID, "error", msg.Error(), "StopTunnel")
			}
			return msg
		}
	}

	// 删除隧道
	delete(tm.tunnels, tunnelID)

	msg := "Tunnel stopped"
	log.Printf("Tunnel %d %s", tunnelID, msg)

	if tm.tunnelService != nil {
		tm.tunnelService.AddTunnelLog(tunnelID, "info", msg, "StopTunnel")
	}

	return nil
}

// acceptConnections 接受外部连接并转发到内部服务
func (tm *TunnelManager) acceptConnections(tunnel *Tunnel) {
	for {
		// 接受外部连接
		conn, err := tunnel.Listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection for tunnel %d: %v", tunnel.ID, err)
			return
		}

		// 处理连接
		go tm.handleConnection(tunnel, conn)
	}
}

// handleConnection 处理单个连接的转发
func (tm *TunnelManager) handleConnection(tunnel *Tunnel, externalConn net.Conn) {
	// 更新连接统计信息
	tunnel.mutex.Lock()
	tunnel.ConnectionCount++
	tunnel.TotalConnections++
	tunnel.LastActive = time.Now()
	tunnel.mutex.Unlock()

	defer externalConn.Close()

	// 连接到本地服务
	localAddr := fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		msg := fmt.Sprintf("Failed to connect to local service %s: %v", localAddr, err)
		log.Printf("Tunnel %d %s", tunnel.ID, msg)
		if tm.tunnelService != nil {
			tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "handleConnection")
		}
		// 更新连接统计信息
		tunnel.mutex.Lock()
		tunnel.ConnectionCount--
		tunnel.mutex.Unlock()
		return
	}
	defer localConn.Close()

	connMsg := fmt.Sprintf("Connection established: %s <-> %s", externalConn.RemoteAddr(), localConn.RemoteAddr())
	log.Printf("Tunnel %d %s", tunnel.ID, connMsg)
	if tm.tunnelService != nil {
		tm.tunnelService.AddTunnelLog(tunnel.ID, "info", connMsg, "handleConnection")
	}

	// 双向数据转发
	var wg sync.WaitGroup
	wg.Add(2)

	// 从外部到本地（接收数据）
	var bytesRecv int64
	go func() {
		defer wg.Done()
		bytes, err := tm.forwardData(externalConn, localConn)
		bytesRecv = bytes
		if err != nil {
			msg := fmt.Sprintf("Error forwarding data external->local: %v", err)
			log.Printf("Tunnel %d %s", tunnel.ID, msg)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "forwardData")
			}
		}
	}()

	// 从本地到外部（发送数据）
	var bytesSent int64
	go func() {
		defer wg.Done()
		bytes, err := tm.forwardData(localConn, externalConn)
		bytesSent = bytes
		if err != nil {
			msg := fmt.Sprintf("Error forwarding data local->external: %v", err)
			log.Printf("Tunnel %d %s", tunnel.ID, msg)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "forwardData")
			}
		}
	}()

	// 等待转发结束
	wg.Wait()

	// 更新字节统计信息
	tunnel.mutex.Lock()
	tunnel.BytesSent += bytesSent
	tunnel.BytesRecv += bytesRecv
	tunnel.ConnectionCount--
	tunnel.LastActive = time.Now()
	tunnel.mutex.Unlock()

	closeMsg := fmt.Sprintf("Connection closed: %s, sent: %d bytes, received: %d bytes",
		externalConn.RemoteAddr(), bytesSent, bytesRecv)
	log.Printf("Tunnel %d %s", tunnel.ID, closeMsg)
	if tm.tunnelService != nil {
		tm.tunnelService.AddTunnelLog(tunnel.ID, "info", closeMsg, "handleConnection")
	}
}

// acceptUDPConnections 接受UDP连接并转发到内部服务
func (tm *TunnelManager) acceptUDPConnections(tunnel *Tunnel) {
	buffer := make([]byte, 65535) // UDP最大数据包大小
	localAddr := fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)
	localUDPAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		msg := fmt.Sprintf("Failed to resolve local UDP address %s: %v", localAddr, err)
		log.Printf("Tunnel %d %s", tunnel.ID, msg)
		if tm.tunnelService != nil {
			tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "acceptUDPConnections")
		}
		return
	}

	for {
		// 接收UDP数据包
		n, remoteAddr, err := tunnel.UDPConn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Failed to read UDP packet for tunnel %d: %v", tunnel.ID, err)
			break
		}

		// 处理UDP数据包
		go tm.handleUDPConnection(tunnel, buffer[:n], remoteAddr, localUDPAddr)
	}
}

// handleUDPConnection 处理UDP连接的转发
func (tm *TunnelManager) handleUDPConnection(tunnel *Tunnel, data []byte, remoteAddr, localUDPAddr *net.UDPAddr) {
	// 更新连接统计信息
	tunnel.mutex.Lock()
	tunnel.TotalConnections++
	tunnel.LastActive = time.Now()
	tunnel.mutex.Unlock()

	// 创建UDP连接到本地服务
	localConn, err := net.DialUDP("udp", nil, localUDPAddr)
	if err != nil {
		msg := fmt.Sprintf("Failed to connect to local UDP service %s: %v", localUDPAddr, err)
		log.Printf("Tunnel %d %s", tunnel.ID, msg)
		if tm.tunnelService != nil {
			tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "handleUDPConnection")
		}
		return
	}
	defer localConn.Close()

	// 将数据发送到本地服务
	bytesSent, err := localConn.Write(data)
	if err != nil {
		msg := fmt.Sprintf("Failed to send UDP data to local service %s: %v", localUDPAddr, err)
		log.Printf("Tunnel %d %s", tunnel.ID, msg)
		if tm.tunnelService != nil {
			tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "handleUDPConnection")
		}
		return
	}

	// 从本地服务接收响应
	buffer := make([]byte, 65535)
	localConn.SetReadDeadline(time.Now().Add(2 * time.Second)) // 设置超时
	bytesRecv, err := localConn.Read(buffer)
	if err != nil {
		// 忽略超时错误，UDP是无连接的，可能没有响应
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			msg := fmt.Sprintf("Failed to read UDP response from local service %s: %v", localUDPAddr, err)
			log.Printf("Tunnel %d %s", tunnel.ID, msg)
			if tm.tunnelService != nil {
				tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "handleUDPConnection")
			}
		}
		return
	}

	// 将响应发送回客户端
	_, err = tunnel.UDPConn.WriteToUDP(buffer[:bytesRecv], remoteAddr)
	if err != nil {
		msg := fmt.Sprintf("Failed to send UDP response to client %s: %v", remoteAddr, err)
		log.Printf("Tunnel %d %s", tunnel.ID, msg)
		if tm.tunnelService != nil {
			tm.tunnelService.AddTunnelLog(tunnel.ID, "error", msg, "handleUDPConnection")
		}
		return
	}

	// 更新统计信息
	tunnel.mutex.Lock()
	tunnel.BytesSent += int64(bytesSent)
	tunnel.BytesRecv += int64(bytesRecv)
	tunnel.mutex.Unlock()
}

// forwardData 数据转发
func (tm *TunnelManager) forwardData(src, dst net.Conn) (int64, error) {
	buffer := make([]byte, 4096)
	total := int64(0)

	for {
		n, err := src.Read(buffer)
		if err != nil {
			return total, err
		}

		if n > 0 {
			m, err := dst.Write(buffer[:n])
			if err != nil {
				return total, err
			}
			total += int64(m)
		}
	}
}

// GetTunnel 获取隧道信息
func (tm *TunnelManager) GetTunnel(tunnelID uint) (*Tunnel, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tunnel, exists := tm.tunnels[tunnelID]
	if !exists {
		return nil, fmt.Errorf("tunnel %d does not exist", tunnelID)
	}

	return tunnel, nil
}

// ListTunnels 列出所有隧道
func (tm *TunnelManager) ListTunnels() []*Tunnel {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var tunnels []*Tunnel
	for _, tunnel := range tm.tunnels {
		tunnels = append(tunnels, tunnel)
	}

	return tunnels
}

// GetTunnelStats 获取隧道统计信息
type TunnelStats struct {
	TunnelID         uint      `json:"tunnel_id"`
	ConnectionCount  int64     `json:"connection_count"`
	TotalConnections int64     `json:"total_connections"`
	BytesSent        int64     `json:"bytes_sent"`
	BytesRecv        int64     `json:"bytes_recv"`
	LastActive       time.Time `json:"last_active"`
	Status           string    `json:"status"`
}

// GetTunnelStats 获取隧道统计信息
func (tm *TunnelManager) GetTunnelStats(tunnelID uint) (*TunnelStats, error) {
	tunnel, err := tm.GetTunnel(tunnelID)
	if err != nil {
		return nil, err
	}

	tunnel.mutex.Lock()
	stats := &TunnelStats{
		TunnelID:         tunnel.ID,
		ConnectionCount:  tunnel.ConnectionCount,
		TotalConnections: tunnel.TotalConnections,
		BytesSent:        tunnel.BytesSent,
		BytesRecv:        tunnel.BytesRecv,
		LastActive:       tunnel.LastActive,
		Status:           tunnel.Status,
	}
	tunnel.mutex.Unlock()

	return stats, nil
}

// GetAllTunnelStats 获取所有隧道统计信息
func (tm *TunnelManager) GetAllTunnelStats() []*TunnelStats {
	tunnels := tm.ListTunnels()
	var stats []*TunnelStats

	for _, tunnel := range tunnels {
		tunnel.mutex.Lock()
		stats = append(stats, &TunnelStats{
			TunnelID:         tunnel.ID,
			ConnectionCount:  tunnel.ConnectionCount,
			TotalConnections: tunnel.TotalConnections,
			BytesSent:        tunnel.BytesSent,
			BytesRecv:        tunnel.BytesRecv,
			LastActive:       tunnel.LastActive,
			Status:           tunnel.Status,
		})
		tunnel.mutex.Unlock()
	}

	return stats
}

// CheckTunnelStatus 检查隧道状态
func (tm *TunnelManager) CheckTunnelStatus(tunnelID uint) error {
	tunnel, err := tm.GetTunnel(tunnelID)
	if err != nil {
		return err
	}

	if tunnel.Type == "udp" {
		// 检查UDP连接是否正常
		if tunnel.UDPConn != nil {
			// UDP连接存在，更新状态为正常
			tunnel.mutex.Lock()
			tunnel.Status = "active"
			tunnel.Error = ""
			tunnel.mutex.Unlock()
		} else {
			// UDP连接不存在，更新状态为非活跃
			tunnel.mutex.Lock()
			tunnel.Status = "inactive"
			tunnel.mutex.Unlock()
		}
	} else {
		// 检查TCP监听器是否正常
		if tunnel.Listener != nil {
			// 尝试创建一个简单的连接来测试隧道是否正常工作
			conn, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", tunnel.RemotePort), 2*time.Second)
			if err != nil {
				// 更新隧道状态为错误
				tunnel.mutex.Lock()
				tunnel.Status = "error"
				tunnel.Error = fmt.Sprintf("Failed to connect to tunnel: %v", err)
				tunnel.mutex.Unlock()
				return err
			}
			conn.Close()

			// 更新隧道状态为正常
			tunnel.mutex.Lock()
			tunnel.Status = "active"
			tunnel.Error = ""
			tunnel.mutex.Unlock()
		} else {
			// 更新隧道状态为非活跃
			tunnel.mutex.Lock()
			tunnel.Status = "inactive"
			tunnel.mutex.Unlock()
		}
	}

	return nil
}

// CheckAllTunnelStatus 检查所有隧道状态
func (tm *TunnelManager) CheckAllTunnelStatus() {
	tunnels := tm.ListTunnels()
	for _, tunnel := range tunnels {
		go tm.CheckTunnelStatus(tunnel.ID)
	}
}

package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// copyBidirectional 双向数据转发
func copyBidirectional(src, dst io.ReadWriteCloser) {
	defer src.Close()
	defer dst.Close()

	// 增加到 128KB 缓冲区，减少系统调用次数
	bufA := make([]byte, 128*1024)
	bufB := make([]byte, 128*1024)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.CopyBuffer(dst, src, bufA)
	}()
	go func() {
		defer wg.Done()
		io.CopyBuffer(src, dst, bufB)
	}()
	wg.Wait()
}

// startTunnelStatusUpdate 启动隧道状态更新机制
func (a *Agent) startTunnelStatusUpdate() {
	// 每30秒更新一次隧道状态
	a.tunnelStatusTicker = time.NewTicker(30 * time.Second)

	go func() {
		for range a.tunnelStatusTicker.C {
			if err := a.updateTunnelStatus(); err != nil {
				log.Printf("Failed to update tunnel status: %v", err)
			}
		}
	}()
}

// updateTunnelStatus 更新隧道状态
func (a *Agent) updateTunnelStatus() error {
	log.Printf("Updating tunnel status...")

	// 如果没有隧道，直接返回
	if len(a.tunnels) == 0 {
		return nil
	}

	// 遍历所有隧道，更新状态
	a.sshMutex.Lock()
	listeners := make(map[uint]net.Listener)
	if a.sshConn != nil {
		for id, listener := range a.sshConn.Listeners {
			listeners[id] = listener
		}
	}
	a.sshMutex.Unlock()

	// 遍历隧道配置
	for _, tunnel := range a.tunnels {
		// 检查隧道是否正在运行
		status := tunnel.Status
		if _, isRunning := listeners[tunnel.ID]; isRunning {
			// 隧道正在运行
			status = "active"
		} else if tunnel.Status == "active" {
			// 隧道配置为active，但实际上没有运行
			status = "error"
		}

		// 如果状态有变化，向服务器报告
		if status != tunnel.Status {
			if err := a.reportTunnelStatus(tunnel.ID, status); err != nil {
				log.Printf("Failed to report tunnel %s status: %v", tunnel.Name, err)
			} else {
				log.Printf("Tunnel %s status updated to %s", tunnel.Name, status)
			}
		}
	}

	return nil
}

// reportTunnelStatus 向服务器报告隧道状态
func (a *Agent) reportTunnelStatus(tunnelID uint, status string) error {
	// 构建状态更新请求
	req := struct {
		TunnelID uint   `json:"tunnel_id"`
		Status   string `json:"status"`
	}{
		TunnelID: tunnelID,
		Status:   status,
	}

	// 发送状态更新请求
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/v1/agent/tunnels/%s/status", a.config.ServerAddr, a.config.AgentID), "application/json", bytes.NewBuffer(reqBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status update failed with status: %d", resp.StatusCode)
	}

	return nil
}

// getSystemInfo 获取系统信息
func getSystemInfo() SystemInfo {
	log.Printf("Collecting system information...")

	// 初始化系统信息
	sysInfo := SystemInfo{}

	// 收集CPU信息
	if cpus, err := cpu.Info(); err == nil && len(cpus) > 0 {
		sysInfo.CPUModel = cpus[0].ModelName
	}

	if cores, err := cpu.Counts(false); err == nil {
		sysInfo.CPUCores = cores
	}

	// 收集内存信息
	if memInfo, err := mem.VirtualMemory(); err == nil {
		sysInfo.MemoryTotal = memInfo.Total
		sysInfo.MemoryUsed = memInfo.Used
	}

	// 收集交换分区信息
	if swapInfo, err := mem.SwapMemory(); err == nil {
		sysInfo.SwapTotal = swapInfo.Total
		sysInfo.SwapUsed = swapInfo.Used
	}

	// 收集系统负载信息
	if loadInfo, err := load.Avg(); err == nil {
		sysInfo.Load1 = loadInfo.Load1
		sysInfo.Load5 = loadInfo.Load5
		sysInfo.Load15 = loadInfo.Load15
	}

	// 收集系统运行时间
	if uptimeInfo, err := host.Uptime(); err == nil {
		sysInfo.Uptime = uint64(uptimeInfo)
	}

	log.Printf("System info collected successfully")
	return sysInfo
}

// getDiskMounts 获取磁盘挂载点信息
func getDiskMounts() []DiskMount {
	log.Printf("Collecting disk mount information...")

	var diskMounts []DiskMount

	// 获取磁盘分区信息
	partitions, err := disk.Partitions(true)
	if err != nil {
		log.Printf("Failed to get disk partitions: %v", err)
		return diskMounts
	}

	// 遍历磁盘分区，收集挂载点信息
	for _, partition := range partitions {
		// 跳过虚拟文件系统和特殊设备
		if partition.Fstype == "tmpfs" || partition.Fstype == "devtmpfs" || partition.Fstype == "sysfs" || partition.Fstype == "proc" {
			continue
		}

		// 获取分区使用情况
		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			log.Printf("Failed to get disk usage for %s: %v", partition.Mountpoint, err)
			continue
		}

		// 创建磁盘挂载点信息对象
		diskMount := DiskMount{
			MountPoint: partition.Mountpoint,
			Total:      usage.Total,
			Used:       usage.Used,
			Free:       usage.Free,
			Fstype:     partition.Fstype,
		}

		diskMounts = append(diskMounts, diskMount)
	}

	log.Printf("Disk mount info collected successfully")
	return diskMounts
}

// register 注册设备
func (a *Agent) register() error {
	log.Printf("Registering agent with server...")

	// 获取本地IP
	ip, err := getLocalIP()
	if err != nil {
		return err
	}

	// 获取操作系统信息
	osInfo := getOSInfo()

	// 收集系统信息
	sysInfo := getSystemInfo()

	// 收集磁盘挂载点信息
	diskMounts := getDiskMounts()

	// 构建注册请求
	req := RegisterRequest{
		AgentID:    a.config.AgentID,
		IPAddress:  ip,
		OS:         osInfo,
		Version:    a.config.Version,
		SystemInfo: sysInfo,
		DiskMounts: diskMounts,
		Token:      a.config.Token,
	}

	// 发送注册请求
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/v1/devices/register", a.config.ServerAddr), "application/json", bytes.NewBuffer(reqBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration failed with status: %d", resp.StatusCode)
	}

	log.Printf("Agent registered successfully")
	return nil
}

// startHeartbeat 启动心跳机制
func (a *Agent) startHeartbeat() {
	a.heartbeatTicker = time.NewTicker(30 * time.Second)

	go func() {
		for range a.heartbeatTicker.C {
			a.sendHeartbeat()
		}
	}()
}

// sendHeartbeat 发送心跳
func (a *Agent) sendHeartbeat() {
	// 构建心跳请求
	req := HeartbeatRequest{
		AgentID: a.config.AgentID,
	}

	// 发送心跳请求
	reqBytes, err := json.Marshal(req)
	if err != nil {
		log.Printf("Failed to marshal heartbeat request: %v", err)
		return
	}

	resp, err := http.Post(fmt.Sprintf("%s/api/v1/devices/heartbeat", a.config.ServerAddr), "application/json", bytes.NewBuffer(reqBytes))
	if err != nil {
		log.Printf("Failed to send heartbeat: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Heartbeat failed with status: %d", resp.StatusCode)
	}
}

// startTunnelConfigUpdate 启动隧道配置更新机制
func (a *Agent) startTunnelConfigUpdate() {
	// 每60秒更新一次隧道配置
	a.tunnelUpdateTicker = time.NewTicker(60 * time.Second)

	go func() {
		for range a.tunnelUpdateTicker.C {
			if err := a.updateTunnelConfigs(); err != nil {
				log.Printf("Failed to update tunnel configs: %v", err)
			} else {
				// 更新隧道配置后，重新同步隧道
				a.syncTunnels()
			}
		}
	}()
}

// startSSHReconnect 启动SSH重连机制
func (a *Agent) startSSHReconnect() {
	// 每30秒尝试重连一次
	a.sshReconnectTicker = time.NewTicker(30 * time.Second)

	go func() {
		for range a.sshReconnectTicker.C {
			a.sshMutex.Lock()
			if a.sshConn == nil || a.sshConn.Client == nil {
				a.sshMutex.Unlock()
				if err := a.connectSSH(); err != nil {
					log.Printf("SSH reconnection failed: %v", err)
				} else {
					// 重连成功后，重新同步隧道
					a.syncTunnels()
				}
			} else {
				a.sshMutex.Unlock()
			}
		}
	}()
}

// connectWebSocket 连接到WebSocket服务器
func (a *Agent) connectWebSocket() error {
	// 将HTTP URL转换为WebSocket URL
	wsURL := a.getWebSocketURL()
	log.Printf("Connecting to WebSocket server: %s", wsURL)

	// 建立WebSocket连接
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Agent-ID": []string{a.config.AgentID},
		"Token":    []string{a.config.Token},
	})
	if err != nil {
		return err
	}

	a.wsMutex.Lock()
	a.wsConn = conn
	a.wsMutex.Unlock()

	log.Printf("WebSocket connected successfully")

	// 启动WebSocket消息处理
	go a.handleWebSocket()
	// 启动WebSocket保活
	go a.wsKeepalive()

	return nil
}

// startWebSocketReconnect 启动WebSocket重连机制
func (a *Agent) startWebSocketReconnect() {
	// 如果已经有重连ticker在运行，先停止
	if a.wsReconnectTicker != nil {
		a.wsReconnectTicker.Stop()
	}

	// 每5秒尝试重连一次
	a.wsReconnectTicker = time.NewTicker(5 * time.Second)

	// 启动重连协程
	go func() {
		for range a.wsReconnectTicker.C {
			a.wsMutex.Lock()
			if a.wsConn == nil {
				a.wsMutex.Unlock()
				// 尝试重连
				if err := a.connectWebSocket(); err != nil {
					log.Printf("WebSocket reconnection failed: %v, will retry...", err)
					continue
				}
				log.Printf("WebSocket reconnected successfully")
				// 重连成功，停止ticker
				a.wsReconnectTicker.Stop()
				a.wsReconnectTicker = nil
				break
			}
			a.wsMutex.Unlock()
		}
	}()
}

// getWebSocketURL 将HTTP URL转换为WebSocket URL
func (a *Agent) getWebSocketURL() string {
	// 替换协议部分
	wsURL := a.config.ServerAddr
	if a.config.ServerAddr[:5] == "http:" {
		wsURL = "ws:" + a.config.ServerAddr[5:]
	} else if a.config.ServerAddr[:6] == "https:" {
		wsURL = "wss:" + a.config.ServerAddr[6:]
	}
	// 添加WebSocket路径
	wsURL += "/ws/agent"
	return wsURL
}

// handleWebSocket 处理WebSocket消息
func (a *Agent) handleWebSocket() {
	for {
		// 读取消息
		messageType, message, err := a.wsConn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			// 关闭连接
			a.wsMutex.Lock()
			a.wsConn.Close()
			a.wsConn = nil
			a.wsMutex.Unlock()
			// 连接断开，重新启动重连机制
			a.startWebSocketReconnect()
			break
		}

		// 处理不同类型的消息
		switch messageType {
		case websocket.TextMessage:
			// 处理文本消息
			a.handleWebSocketTextMessage(message)
		case websocket.BinaryMessage:
			// 处理二进制消息（用于SSH数据传输）
			a.handleWebSocketBinaryMessage(message)
		}
	}
}

// handleWebSocketTextMessage 处理WebSocket文本消息
func (a *Agent) handleWebSocketTextMessage(message []byte) {
	log.Printf("Received WebSocket text message: %s", string(message))
	// 目前只处理二进制消息用于SSH数据传输
}

// handleWebSocketBinaryMessage 处理WebSocket二进制消息（SSH数据）
func (a *Agent) handleWebSocketBinaryMessage(message []byte) {
	// 解析消息头，获取终端ID和操作类型
	// 简单的消息格式：[terminalID][opCode][data]
	if len(message) < 2 {
		log.Printf("Invalid WebSocket binary message: too short")
		return
	}

	terminalID := message[0]
	opCode := message[1]
	data := message[2:]

	switch opCode {
	case 0x00: // SSH连接请求
		a.handleSSHConnectRequest(terminalID, data)
	case 0x01: // SSH数据
		a.handleSSHData(terminalID, data)
	case 0x02: // SSH断开连接
		a.handleSSHDisconnect(terminalID, data)
	default:
		log.Printf("Unknown WebSocket opCode: %d", opCode)
	}
}

// handleSSHConnectRequest 处理SSH连接请求
func (a *Agent) handleSSHConnectRequest(terminalID byte, data []byte) {
	// 解析SSH连接信息
	var sshReq struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Port     int    `json:"port"`
		Cols     int    `json:"cols"`
		Rows     int    `json:"rows"`
	}

	if err := json.Unmarshal(data, &sshReq); err != nil {
		log.Printf("Failed to parse SSH connect request: %v", err)
		// 返回错误
		errMsg := []byte{terminalID, 0x03, 0x01} // 0x03=error, 0x01=parse error
		a.sendWebSocketBinaryMessage(errMsg)
		return
	}

	// 连接到本地SSH服务器
	sshConfig := &ssh.ClientConfig{
		User:            sshReq.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(sshReq.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", sshReq.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		log.Printf("Failed to connect to local SSH server: %v", err)
		// 返回错误
		errMsg := []byte{terminalID, 0x03, 0x02} // 0x03=error, 0x02=connect error
		a.sendWebSocketBinaryMessage(errMsg)
		return
	}

	// 创建SSH会话
	session, err := client.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session: %v", err)
		client.Close()
		// 返回错误
		errMsg := []byte{terminalID, 0x03, 0x03} // 0x03=error, 0x03=session error
		a.sendWebSocketBinaryMessage(errMsg)
		return
	}

	// 设置终端尺寸默认值
	cols := sshReq.Cols
	rows := sshReq.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	// 配置终端模式，确保正确处理信号
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,      // 开启回显
		ssh.TTY_OP_ISPEED: 115200, // 输入速度
		ssh.TTY_OP_OSPEED: 115200, // 输出速度
		ssh.ICANON:        1,      // 启用规范模式
		ssh.IEXTEN:        1,      // 启用扩展输入处理
		ssh.ISIG:          1,      // 启用信号
		ssh.IXON:          1,      // 启用XON/XOFF流控制
		ssh.IXOFF:         1,      // 启用XON/XOFF输入流控制
		ssh.ISTRIP:        0,      // 不剥离第八位
		ssh.PARMRK:        0,      // 不标记奇偶校验错误
		ssh.INPCK:         0,      // 禁用奇偶校验检查
		ssh.OPOST:         1,      // 启用输出处理
	}

	// 打开PTY，使用客户端传递的终端尺寸和正确的终端模式
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		log.Printf("Failed to request PTY: %v", err)
		session.Close()
		client.Close()
		// 返回错误
		errMsg := []byte{terminalID, 0x03, 0x04} // 0x03=error, 0x04=pty error
		a.sendWebSocketBinaryMessage(errMsg)
		return
	}

	// 获取标准输入输出
	stdin, err := session.StdinPipe()
	if err != nil {
		log.Printf("Failed to get stdin pipe: %v", err)
		session.Close()
		client.Close()
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Printf("Failed to get stdout pipe: %v", err)
		session.Close()
		client.Close()
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		log.Printf("Failed to get stderr pipe: %v", err)
		session.Close()
		client.Close()
		return
	}

	// 启动会话
	if err := session.Shell(); err != nil {
		log.Printf("Failed to start shell: %v", err)
		session.Close()
		client.Close()
		return
	}

	// 保存会话到会话管理
	terminalMutex.Lock()
	terminalSessions[terminalID] = &TerminalSession{
		stdin:   stdin,
		session: session,
		client:  client,
	}
	terminalMutex.Unlock()

	// 返回成功
	successMsg := []byte{terminalID, 0x04} // 0x04=success
	a.sendWebSocketBinaryMessage(successMsg)

	// 启动数据转发协程
	go a.forwardSSHData(terminalID, stdin, stdout, stderr, session, client)
}

// 终端会话管理
var (
	terminalSessions = make(map[byte]*TerminalSession)
	terminalMutex    sync.Mutex
)

// TerminalSession 终端会话
type TerminalSession struct {
	stdin   io.WriteCloser
	session *ssh.Session
	client  *ssh.Client
}

// handleSSHData 处理SSH数据
func (a *Agent) handleSSHData(terminalID byte, data []byte) {
	log.Printf("Received SSH data for terminal %d: %d bytes", terminalID, len(data))

	// 获取会话
	terminalMutex.Lock()
	session, exists := terminalSessions[terminalID]
	terminalMutex.Unlock()

	if !exists {
		log.Printf("Terminal session %d not found", terminalID)
		return
	}

	// 写入数据到SSH stdin
	if _, err := session.stdin.Write(data); err != nil {
		log.Printf("Failed to write SSH data for terminal %d: %v", terminalID, err)
		// 发送错误通知
		errMsg := []byte{terminalID, 0x03, 0x05} // 0x05=write error
		a.sendWebSocketBinaryMessage(errMsg)
	}
}

// handleSSHDisconnect 处理SSH断开连接
func (a *Agent) handleSSHDisconnect(terminalID byte, data []byte) {
	log.Printf("Received SSH disconnect for terminal %d", terminalID)

	// 获取会话
	terminalMutex.Lock()
	session, exists := terminalSessions[terminalID]
	if exists {
		// 关闭会话和客户端
		session.session.Close()
		session.client.Close()
		// 从会话管理中移除
		delete(terminalSessions, terminalID)
	}
	terminalMutex.Unlock()

	// 发送断开连接确认
	disconnectMsg := []byte{terminalID, 0x05} // 0x05=disconnected
	a.sendWebSocketBinaryMessage(disconnectMsg)
}

// forwardSSHData 转发SSH数据
func (a *Agent) forwardSSHData(terminalID byte, stdin io.WriteCloser, stdout, stderr io.Reader, session *ssh.Session, client *ssh.Client) {
	defer func() {
		session.Close()
		client.Close()
		// 从会话管理中移除
		terminalMutex.Lock()
		delete(terminalSessions, terminalID)
		terminalMutex.Unlock()
		// 发送断开连接通知
		disconnectMsg := []byte{terminalID, 0x05} // 0x05=disconnected
		a.sendWebSocketBinaryMessage(disconnectMsg)
	}()

	// 从stdout读取数据并发送到WebSocket
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := stdout.Read(buffer)
			if err != nil {
				break
			}
			if n > 0 {
				// 构建消息：[terminalID][opCode=0x01][data]
				msg := make([]byte, 2+n)
				msg[0] = terminalID
				msg[1] = 0x01
				copy(msg[2:], buffer[:n])
				a.sendWebSocketBinaryMessage(msg)
			}
		}
	}()

	// 从stderr读取数据并发送到WebSocket
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := stderr.Read(buffer)
			if err != nil {
				break
			}
			if n > 0 {
				// 构建消息：[terminalID][opCode=0x01][data]
				msg := make([]byte, 2+n)
				msg[0] = terminalID
				msg[1] = 0x01
				copy(msg[2:], buffer[:n])
				a.sendWebSocketBinaryMessage(msg)
			}
		}
	}()

	// 等待会话结束
	session.Wait()
}

// sendWebSocketBinaryMessage 发送WebSocket二进制消息
func (a *Agent) sendWebSocketBinaryMessage(message []byte) {
	a.wsMutex.Lock()
	defer a.wsMutex.Unlock()

	if a.wsConn != nil {
		// 确保消息格式正确，只包含终端ID、操作码和数据
		if len(message) < 2 {
			log.Printf("Invalid WebSocket message: too short")
			return
		}

		// 检查操作码是否有效
		opCode := message[1]
		validOpCodes := map[byte]bool{
			0x00: true, // SSH连接请求
			0x01: true, // SSH数据
			0x02: true, // SSH断开连接
			0x03: true, // SSH错误
			0x04: true, // SSH连接成功
			0x05: true, // SSH断开连接通知
		}

		if !validOpCodes[opCode] {
			log.Printf("Invalid WebSocket opcode: %d", opCode)
			return
		}

		// 发送消息
		if err := a.wsConn.WriteMessage(websocket.BinaryMessage, message); err != nil {
			log.Printf("Failed to send WebSocket message: %v", err)
			// 关闭连接
			a.wsConn.Close()
			a.wsConn = nil
		}
	}
}

// wsKeepalive WebSocket保活机制
func (a *Agent) wsKeepalive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.wsMutex.Lock()
			conn := a.wsConn
			a.wsMutex.Unlock()

			if conn != nil {
				// 发送ping消息
				if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
					log.Printf("WebSocket ping failed: %v", err)
					// 关闭连接
					a.wsMutex.Lock()
					a.wsConn.Close()
					a.wsConn = nil
					a.wsMutex.Unlock()
					return
				}
			} else {
				return
			}
		}
	}
}

// connectSSH 连接到SSH服务器
func (a *Agent) connectSSH() error {
	log.Printf("Connecting to SSH server %s:%d...", a.sshConfig.Host, a.sshConfig.Port)

	// 配置SSH客户端
	sshConfig := &ssh.ClientConfig{
		User:            a.sshConfig.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(a.sshConfig.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	// 连接到SSH服务器
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", a.sshConfig.Host, a.sshConfig.Port), sshConfig)
	if err != nil {
		return err
	}

	// 创建SSH连接信息
	a.sshMutex.Lock()
	a.sshConn = &SSHConnection{
		Client:    client,
		Listeners: make(map[uint]net.Listener),
	}
	a.sshMutex.Unlock()

	log.Printf("SSH connected successfully to %s:%d", a.sshConfig.Host, a.sshConfig.Port)

	// 启动keepalive机制
	go a.sshKeepalive()

	return nil
}

// sshKeepalive SSH保活机制
func (a *Agent) sshKeepalive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.sshMutex.Lock()
			client := a.sshConn.Client
			a.sshMutex.Unlock()

			if client != nil {
				if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					log.Printf("SSH keepalive failed: %v", err)
					// 连接已断开，清除SSH连接
					a.sshMutex.Lock()
					a.sshConn = nil
					a.sshMutex.Unlock()
					return
				}
			} else {
				return
			}
		}
	}
}

// syncTunnels 同步隧道配置
func (a *Agent) syncTunnels() {
	log.Printf("Syncing tunnels...")

	// 检查SSH连接是否可用
	var client *ssh.Client
	a.sshMutex.Lock()
	if a.sshConn != nil {
		client = a.sshConn.Client
	}
	a.sshMutex.Unlock()

	if client == nil {
		// SSH连接不可用，跳过隧道同步
		log.Printf("SSH connection not available, skipping tunnel sync")
		return
	}

	// 遍历隧道配置，建立或关闭隧道
	for _, tunnel := range a.tunnels {
		if tunnel.Status == "active" {
			// 启动隧道
			if err := a.startTunnel(tunnel); err != nil {
				log.Printf("Failed to start tunnel %s: %v", tunnel.Name, err)
			} else {
				log.Printf("Tunnel %s started: %s:%d -> %d", tunnel.Name, tunnel.LocalIP, tunnel.LocalPort, tunnel.RemotePort)
			}
		} else {
			// 关闭隧道
			a.stopTunnel(tunnel)
			log.Printf("Tunnel %s stopped", tunnel.Name)
		}
	}
}

// startTunnel 启动隧道
func (a *Agent) startTunnel(tunnel *TunnelConfig) error {
	a.sshMutex.Lock()
	defer a.sshMutex.Unlock()

	// 检查SSH连接是否可用
	if a.sshConn == nil || a.sshConn.Client == nil {
		return fmt.Errorf("SSH connection not available")
	}

	// 检查隧道是否已启动
	if _, exists := a.sshConn.Listeners[tunnel.ID]; exists {
		return fmt.Errorf("tunnel %s already running", tunnel.Name)
	}

	// 建立SSH隧道
	listener, err := a.sshConn.Client.Listen("tcp", fmt.Sprintf(":%d", tunnel.RemotePort))
	if err != nil {
		return err
	}

	// 保存监听器
	a.sshConn.Listeners[tunnel.ID] = listener

	// 启动隧道处理协程
	go a.handleTunnel(tunnel, listener)

	return nil
}

// stopTunnel 停止隧道
func (a *Agent) stopTunnel(tunnel *TunnelConfig) {
	a.sshMutex.Lock()
	defer a.sshMutex.Unlock()

	if a.sshConn != nil {
		// 检查隧道是否已启动
		if listener, exists := a.sshConn.Listeners[tunnel.ID]; exists {
			// 关闭监听器
			listener.Close()
			// 移除监听器
			delete(a.sshConn.Listeners, tunnel.ID)
		}
	}
}

// handleTunnel 处理隧道连接
func (a *Agent) handleTunnel(tunnel *TunnelConfig, listener net.Listener) {
	defer listener.Close()

	log.Printf("Tunnel %s listening on remote port %d", tunnel.Name, tunnel.RemotePort)

	for {
		// 接受远程连接
		remoteConn, err := listener.Accept()
		if err != nil {
			log.Printf("Tunnel %s accept error: %v", tunnel.Name, err)
			break
		}

		// 连接到本地服务
		localAddr := fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)
		localConn, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
		if err != nil {
			log.Printf("Tunnel %s failed to connect to local service %s: %v", tunnel.Name, localAddr, err)
			remoteConn.Close()
			continue
		}

		// 设置NoDelay
		if tcpConn, ok := localConn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
		}

		log.Printf("Tunnel %s connection established: %s <-> %s", tunnel.Name, remoteConn.RemoteAddr(), localAddr)

		// 双向转发数据
		go copyBidirectional(remoteConn, localConn)
	}
}

// updateTunnelConfigs 更新隧道配置
func (a *Agent) updateTunnelConfigs() error {
	log.Printf("Updating tunnel configurations...")

	// 发送GET请求获取隧道配置
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/agent/tunnels/%s", a.config.ServerAddr, a.config.AgentID))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get tunnel configs with status: %d", resp.StatusCode)
	}

	// 解析响应
	var result struct {
		Code  int            `json:"code"`
		Msg   string         `json:"msg"`
		Count int            `json:"count"`
		Data  []TunnelConfig `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// 更新隧道配置
	newTunnels := make(map[uint]*TunnelConfig)
	for i := range result.Data {
		config := result.Data[i]
		newTunnels[config.ID] = &config
		log.Printf("Tunnel config: %+v", config)
	}

	// 更新Agent的隧道配置
	a.tunnels = newTunnels

	log.Printf("Updated %d tunnel configurations successfully", len(result.Data))
	return nil
}

// getAgentID 获取或生成Agent ID
func getAgentID() string {
	// 获取Agent ID文件路径
	agentIDFile := getAgentIDFile()
	log.Printf("Using agent ID file: %s", agentIDFile)

	// 检查是否存在Agent ID文件
	if _, err := os.Stat(agentIDFile); err == nil {
		// 读取现有Agent ID
		content, err := os.ReadFile(agentIDFile)
		if err == nil && len(content) > 0 {
			agentID := string(bytes.TrimSpace(content))
			log.Printf("Loaded existing agent ID: %s", agentID[:8]+"...")
			return agentID
		}
	}

	// 生成新的Agent ID
	newID := uuid.New().String()
	log.Printf("Generated new agent ID: %s", newID[:8]+"...")

	// 确保目录存在
	agentIDDir := filepath.Dir(agentIDFile)
	if _, err := os.Stat(agentIDDir); os.IsNotExist(err) {
		if err := os.MkdirAll(agentIDDir, 0755); err != nil {
			// 如果创建目录失败，尝试使用当前目录
			agentIDFile = "./agent_id"
			log.Printf("Falling back to current directory for agent ID file: %s", agentIDFile)
		} else {
			// 成功创建/etc/tbore，使用该目录
			agentIDFile = "/etc/tbore/agent_id"
		}
	}

	// 保存Agent ID到文件
	if err := os.WriteFile(agentIDFile, []byte(newID), 0600); err != nil {
		log.Printf("Failed to save agent ID: %v", err)
		// 如果保存失败，仍然返回生成的ID，但会在下次启动时重新生成
	}

	return newID
}

// getAgentIDFile 获取Agent ID文件路径
func getAgentIDFile() string {
	var agentIDFile string

	// 根据操作系统选择不同的配置目录
	if isWindows() {
		// Windows: 使用AppData目录
		appData := os.Getenv("APPDATA")
		if appData != "" {
			agentIDFile = filepath.Join(appData, "tbore", "agent_id")
		} else {
			agentIDFile = "./agent_id"
		}
	} else if isMac() {
		// macOS: 使用Library/Application Support目录
		home := os.Getenv("HOME")
		if home != "" {
			agentIDFile = filepath.Join(home, "Library", "Application Support", "tbore", "agent_id")
		} else {
			agentIDFile = "./agent_id"
		}
	} else {
		// Linux/Unix: 使用/etc目录或~/.tbore目录
		if _, err := os.Stat("/etc/tbore"); err == nil || os.IsNotExist(err) {
			// 如果/etc/tbore存在或可以创建
			if os.IsNotExist(err) {
				if err := os.MkdirAll("/etc/tbore", 0755); err != nil {
					// 如果无法创建/etc/tbore，使用~/.tbore
					home := os.Getenv("HOME")
					if home != "" {
						agentIDFile = filepath.Join(home, ".tbore", "agent_id")
					} else {
						agentIDFile = "./agent_id"
					}
				} else {
					// 成功创建/etc/tbore，使用该目录
					agentIDFile = "/etc/tbore/agent_id"
				}
			} else {
				// /etc/tbore已经存在，使用该目录
				agentIDFile = "/etc/tbore/agent_id"
			}
		} else {
			// 无法访问/etc/tbore，使用~/.tbore
			home := os.Getenv("HOME")
			if home != "" {
				agentIDFile = filepath.Join(home, ".tbore", "agent_id")
			} else {
				agentIDFile = "./agent_id"
			}
		}
	}

	return agentIDFile
}

// getLocalIP 获取本地IP地址
func getLocalIP() (string, error) {
	// 执行ifconfig或ip命令获取IP地址
	var cmd *exec.Cmd
	if isLinux() {
		cmd = exec.Command("hostname", "-i")
	} else if isWindows() {
		cmd = exec.Command("powershell", "(Get-NetIPAddress -AddressFamily IPv4 -InterfaceAlias Ethernet).IPAddress")
	} else {
		return "127.0.0.1", nil
	}

	output, err := cmd.Output()
	if err != nil {
		return "127.0.0.1", nil
	}

	ip := string(bytes.TrimSpace(output))
	if ip == "" {
		return "127.0.0.1", nil
	}

	return ip, nil
}

// getOSInfo 获取操作系统信息
func getOSInfo() string {
	os := runtime.GOOS
	switch os {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	default:
		return fmt.Sprintf("%s", os)
	}
}

// isLinux 检查是否为Linux系统
func isLinux() bool {
	return runtime.GOOS == "linux"
}

// isWindows 检查是否为Windows系统
func isWindows() bool {
	return runtime.GOOS == "windows"
}

// isMac 检查是否为macOS系统
func isMac() bool {
	return runtime.GOOS == "darwin"
}
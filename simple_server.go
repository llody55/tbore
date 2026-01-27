package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

// Config 简化版配置
type Config struct {
	Port   int
	DBPath string
}

// Token 模型
type Token struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Device 模型
type Device struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	AgentID       string    `json:"agent_id"`
	IPAddress     string    `json:"ip_address"`
	OS            string    `json:"os"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Tunnel 隧道模型
type Tunnel struct {
	ID         uint         `json:"id"`
	DeviceID   uint         `json:"device_id"`
	Name       string       `json:"name"`
	Type       string       `json:"type"`
	LocalIP    string       `json:"local_ip"`
	LocalPort  int          `json:"local_port"`
	RemotePort int          `json:"remote_port"`
	Status     string       `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Listener   net.Listener `json:"-"` // 不序列化到JSON
}

var (
	db            *sql.DB
	config        Config
	tunnelManager *TunnelManager
)

// TunnelManager 隧道管理器
type TunnelManager struct {
	mu      sync.Mutex
	tunnels map[uint]*Tunnel
}

// NewTunnelManager 创建新的隧道管理器
func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make(map[uint]*Tunnel),
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

	// 监听远程端口
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", remotePort))
	if err != nil {
		return fmt.Errorf("failed to listen on remote port %d: %v", remotePort, err)
	}

	// 创建隧道实例
	tunnel := &Tunnel{
		ID:         tunnelID,
		DeviceID:   deviceID,
		Name:       name,
		Type:       tunnelType,
		LocalIP:    localIP,
		LocalPort:  localPort,
		RemotePort: remotePort,
		Status:     "active",
		Listener:   listener,
	}

	// 保存隧道
	tm.tunnels[tunnelID] = tunnel

	// 启动监听协程
	go tm.acceptConnections(tunnel)

	log.Printf("Tunnel %d started: %s:%d -> %s:%d (remote port %d)",
		tunnelID, localIP, localPort, tunnelType, remotePort, remotePort)

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

	// 关闭监听器
	if err := tunnel.Listener.Close(); err != nil {
		return fmt.Errorf("failed to close listener for tunnel %d: %v", tunnelID, err)
	}

	// 删除隧道
	delete(tm.tunnels, tunnelID)

	log.Printf("Tunnel %d stopped", tunnelID)

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
	defer externalConn.Close()

	// 连接到本地服务
	localAddr := fmt.Sprintf("%s:%d", tunnel.LocalIP, tunnel.LocalPort)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("Failed to connect to local service %s for tunnel %d: %v", localAddr, tunnel.ID, err)
		return
	}
	defer localConn.Close()

	log.Printf("Connection established for tunnel %d: %s <-> %s",
		tunnel.ID, externalConn.RemoteAddr(), localConn.RemoteAddr())

	// 双向数据转发
	var wg sync.WaitGroup
	wg.Add(2)

	// 从外部到本地
	go func() {
		defer wg.Done()
		if _, err := tm.forwardData(externalConn, localConn); err != nil {
			log.Printf("Error forwarding data external->local for tunnel %d: %v", tunnel.ID, err)
		}
	}()

	// 从本地到外部
	go func() {
		defer wg.Done()
		if _, err := tm.forwardData(localConn, externalConn); err != nil {
			log.Printf("Error forwarding data local->external for tunnel %d: %v", tunnel.ID, err)
		}
	}()

	// 等待转发结束
	wg.Wait()

	log.Printf("Connection closed for tunnel %d: %s", tunnel.ID, externalConn.RemoteAddr())
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

func main() {
	// 初始化配置
	config = Config{
		Port:   7835,
		DBPath: "./tbore.db",
	}

	// 初始化隧道管理器
	tunnelManager = NewTunnelManager()

	// 初始化数据库
	initDB()
	defer db.Close()

	// 创建路由
	r := mux.NewRouter()

	// API路由
	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/tokens", getTokens).Methods("GET")
	api.HandleFunc("/tokens", createToken).Methods("POST")
	api.HandleFunc("/tokens/{id}", getToken).Methods("GET")
	api.HandleFunc("/tokens/{id}", updateToken).Methods("PUT")
	api.HandleFunc("/tokens/{id}", deleteToken).Methods("DELETE")
	api.HandleFunc("/tokens/{id}/status", updateTokenStatus).Methods("PUT")
	api.HandleFunc("/tokens/validate", validateToken).Methods("POST")

	// 设备管理API
	api.HandleFunc("/devices/register", registerDevice).Methods("POST")
	api.HandleFunc("/devices/heartbeat", updateHeartbeat).Methods("POST")
	api.HandleFunc("/devices", getDevices).Methods("GET")
	api.HandleFunc("/devices/{id}", getDevice).Methods("GET")
	api.HandleFunc("/devices/{id}", updateDevice).Methods("PUT")

	// 隧道管理API
	api.HandleFunc("/tunnels", getTunnels).Methods("GET")
	api.HandleFunc("/tunnels", createTunnel).Methods("POST")
	api.HandleFunc("/tunnels/{id}", getTunnel).Methods("GET")
	api.HandleFunc("/tunnels/{id}", updateTunnel).Methods("PUT")
	api.HandleFunc("/tunnels/{id}", deleteTunnel).Methods("DELETE")
	api.HandleFunc("/tunnels/{id}/status", updateTunnelStatus).Methods("PUT")
	api.HandleFunc("/tunnels/device/{deviceId}", getTunnelsByDevice).Methods("GET")

	// 静态文件服务
	r.PathPrefix("/web").Handler(http.StripPrefix("/web", http.FileServer(http.Dir("./web"))))
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/", http.StatusFound)
	})

	// 启动服务器
	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Server started on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

// 初始化数据库
func initDB() {
	var err error
	db, err = sql.Open("sqlite3", config.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// 创建表
	createTables()

	// 初始化默认数据
	initDefaultData()
}

// 创建数据库表
func createTables() {
	// 创建tokens表
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			value TEXT NOT NULL UNIQUE,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create tokens table: %v", err)
	}

	// 创建devices表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS devices (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			agent_id TEXT NOT NULL UNIQUE,
			ip_address TEXT NOT NULL,
			os TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'offline',
			last_heartbeat DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create devices table: %v", err)
	}

	// 创建tunnels表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tunnels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'tcp',
			local_ip TEXT NOT NULL,
			local_port INTEGER NOT NULL,
			remote_port INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'inactive',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (device_id) REFERENCES devices(id)
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create tunnels table: %v", err)
	}
}

// 初始化默认数据
func initDefaultData() {
	// 检查是否已有数据
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tokens").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to check tokens: %v", err)
	}

	if count == 0 {
		// 插入默认Token
		_, err := db.Exec(`
			INSERT INTO tokens (name, value, description, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`, "default", "default-token-123456", "Default token for testing", "active")
		if err != nil {
			log.Fatalf("Failed to insert default token: %v", err)
		}
		log.Println("Created default token: default-token-123456")
	}
}

// 获取所有Token
func getTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name, value, description, status, created_at, updated_at FROM tokens ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tokens []Token
	for rows.Next() {
		var t Token
		err := rows.Scan(&t.ID, &t.Name, &t.Value, &t.Description, &t.Status, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tokens = append(tokens, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":  200,
		"msg":   "",
		"count": len(tokens),
		"data":  tokens,
	})
}

// 创建新Token
func createToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 生成简单Token
	tokenValue := fmt.Sprintf("token-%d-%d", time.Now().Unix(), os.Getpid())

	result, err := db.Exec(`
		INSERT INTO tokens (name, value, description, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, req.Name, tokenValue, req.Description, "active")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	// 返回创建的Token
	token := Token{
		ID:          int(id),
		Name:        req.Name,
		Value:       tokenValue,
		Description: req.Description,
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(token)
}

// 获取单个Token
func getToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var token Token
	err := db.QueryRow("SELECT id, name, value, description, status, created_at, updated_at FROM tokens WHERE id = ?", id).Scan(
		&token.ID, &token.Name, &token.Value, &token.Description, &token.Status, &token.CreatedAt, &token.UpdatedAt)
	if err != nil {
		http.Error(w, "Token not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(token)
}

// 更新Token信息
func updateToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 更新Token
	_, err := db.Exec(`
		UPDATE tokens SET name = ?, description = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Name, req.Description, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回更新后的Token
	var updatedToken Token
	err = db.QueryRow("SELECT id, name, value, description, status, created_at, updated_at FROM tokens WHERE id = ?", id).Scan(
		&updatedToken.ID, &updatedToken.Name, &updatedToken.Value, &updatedToken.Description, &updatedToken.Status, &updatedToken.CreatedAt, &updatedToken.UpdatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedToken)
}

// 更新Token状态（启用/禁用）
func updateTokenStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 验证状态值
	if req.Status != "active" && req.Status != "inactive" {
		http.Error(w, "Invalid status. Must be 'active' or 'inactive'", http.StatusBadRequest)
		return
	}

	// 更新Token状态
	_, err := db.Exec(`
		UPDATE tokens SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Status, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回更新后的Token
	var updatedToken Token
	err = db.QueryRow("SELECT id, name, value, description, status, created_at, updated_at FROM tokens WHERE id = ?", id).Scan(
		&updatedToken.ID, &updatedToken.Name, &updatedToken.Value, &updatedToken.Description, &updatedToken.Status, &updatedToken.CreatedAt, &updatedToken.UpdatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedToken)
}

// 删除Token
func deleteToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := db.Exec("DELETE FROM tokens WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// 获取单个设备
func getDevice(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var device Device
	err := db.QueryRow("SELECT id, name, agent_id, ip_address, os, status, last_heartbeat, created_at, updated_at FROM devices WHERE id = ?", id).Scan(
		&device.ID, &device.Name, &device.AgentID, &device.IPAddress, &device.OS, &device.Status, &device.LastHeartbeat, &device.CreatedAt, &device.UpdatedAt)
	if err != nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

// 更新设备信息
func updateDevice(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 更新设备
	_, err := db.Exec(`
		UPDATE devices SET name = ?, status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Name, req.Status, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回更新后的设备
	var updatedDevice Device
	err = db.QueryRow("SELECT id, name, agent_id, ip_address, os, status, last_heartbeat, created_at, updated_at FROM devices WHERE id = ?", id).Scan(
		&updatedDevice.ID, &updatedDevice.Name, &updatedDevice.AgentID, &updatedDevice.IPAddress, &updatedDevice.OS, &updatedDevice.Status, &updatedDevice.LastHeartbeat, &updatedDevice.CreatedAt, &updatedDevice.UpdatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedDevice)
}

// 验证Token
func validateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tokens WHERE value = ? AND status = ?", req.Token, "active").Scan(&count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	valid := count > 0
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"valid": valid})
}

// 设备注册
func registerDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID   string `json:"agent_id"`
		IPAddress string `json:"ip_address"`
		OS        string `json:"os"`
		Token     string `json:"token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 验证Token
	var tokenValid bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM tokens WHERE value = ? AND status = ?)", req.Token, "active").Scan(&tokenValid)
	if err != nil || !tokenValid {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// 检查设备是否已存在
	var existingDevice Device
	err = db.QueryRow("SELECT id, name, agent_id, ip_address, os, status, last_heartbeat, created_at, updated_at FROM devices WHERE agent_id = ?", req.AgentID).Scan(
		&existingDevice.ID, &existingDevice.Name, &existingDevice.AgentID, &existingDevice.IPAddress, &existingDevice.OS,
		&existingDevice.Status, &existingDevice.LastHeartbeat, &existingDevice.CreatedAt, &existingDevice.UpdatedAt)

	if err == nil {
		// 设备已存在，更新信息
		_, err = db.Exec(`
			UPDATE devices SET ip_address = ?, os = ?, status = ?, last_heartbeat = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE agent_id = ?
		`, req.IPAddress, req.OS, "online", req.AgentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// 返回更新后的设备信息
		existingDevice.IPAddress = req.IPAddress
		existingDevice.OS = req.OS
		existingDevice.Status = "online"
		existingDevice.LastHeartbeat = time.Now()
		existingDevice.UpdatedAt = time.Now()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(existingDevice)
		return
	}

	// 创建新设备
	deviceName := fmt.Sprintf("Device-%s", req.AgentID[:8])
	result, err := db.Exec(`
		INSERT INTO devices (name, agent_id, ip_address, os, status, last_heartbeat, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, deviceName, req.AgentID, req.IPAddress, req.OS, "online")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	device := Device{
		ID:            int(id),
		Name:          deviceName,
		AgentID:       req.AgentID,
		IPAddress:     req.IPAddress,
		OS:            req.OS,
		Status:        "online",
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(device)
}

// 更新设备心跳
func updateHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID string `json:"agent_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 更新心跳
	result, err := db.Exec(`
		UPDATE devices SET status = ?, last_heartbeat = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE agent_id = ?
	`, "online", req.AgentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// 获取所有设备
func getDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name, agent_id, ip_address, os, status, last_heartbeat, created_at, updated_at FROM devices ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		err := rows.Scan(&d.ID, &d.Name, &d.AgentID, &d.IPAddress, &d.OS, &d.Status, &d.LastHeartbeat, &d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		devices = append(devices, d)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":  200,
		"msg":   "",
		"count": len(devices),
		"data":  devices,
	})
}

// 获取所有隧道
func getTunnels(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, device_id, name, type, local_ip, local_port, remote_port, status, created_at, updated_at FROM tunnels ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tunnels []Tunnel
	for rows.Next() {
		var t Tunnel
		var id, deviceID int
		err := rows.Scan(&id, &deviceID, &t.Name, &t.Type, &t.LocalIP, &t.LocalPort, &t.RemotePort, &t.Status, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t.ID = uint(id)
		t.DeviceID = uint(deviceID)
		tunnels = append(tunnels, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":  200,
		"msg":   "",
		"count": len(tunnels),
		"data":  tunnels,
	})
}

// 创建新隧道
func createTunnel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID   uint   `json:"device_id"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		LocalIP    string `json:"local_ip"`
		LocalPort  int    `json:"local_port"`
		RemotePort int    `json:"remote_port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 验证必填字段
	if req.Name == "" || req.LocalIP == "" || req.LocalPort == 0 || req.RemotePort == 0 {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// 如果类型未指定，默认为tcp
	if req.Type == "" {
		req.Type = "tcp"
	}

	result, err := db.Exec(`
		INSERT INTO tunnels (device_id, name, type, local_ip, local_port, remote_port, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, req.DeviceID, req.Name, req.Type, req.LocalIP, req.LocalPort, req.RemotePort, "inactive")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	// 返回创建的隧道
	tunnel := Tunnel{
		ID:         uint(id),
		DeviceID:   req.DeviceID,
		Name:       req.Name,
		Type:       req.Type,
		LocalIP:    req.LocalIP,
		LocalPort:  req.LocalPort,
		RemotePort: req.RemotePort,
		Status:     "inactive",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(tunnel)
}

// 获取单个隧道
func getTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var tunnel Tunnel
	var dbID, dbDeviceID int
	err := db.QueryRow("SELECT id, device_id, name, type, local_ip, local_port, remote_port, status, created_at, updated_at FROM tunnels WHERE id = ?", id).Scan(
		&dbID, &dbDeviceID, &tunnel.Name, &tunnel.Type, &tunnel.LocalIP, &tunnel.LocalPort, &tunnel.RemotePort, &tunnel.Status, &tunnel.CreatedAt, &tunnel.UpdatedAt)
	if err != nil {
		http.Error(w, "Tunnel not found", http.StatusNotFound)
		return
	}

	// 转换为uint
	tunnel.ID = uint(dbID)
	tunnel.DeviceID = uint(dbDeviceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tunnel)
}

// 更新隧道信息
func updateTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		DeviceID   uint   `json:"device_id"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		LocalIP    string `json:"local_ip"`
		LocalPort  int    `json:"local_port"`
		RemotePort int    `json:"remote_port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 验证必填字段
	if req.Name == "" || req.LocalIP == "" || req.LocalPort == 0 || req.RemotePort == 0 {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// 更新隧道信息
	_, err := db.Exec(`
		UPDATE tunnels SET device_id = ?, name = ?, type = ?, local_ip = ?, local_port = ?, remote_port = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.DeviceID, req.Name, req.Type, req.LocalIP, req.LocalPort, req.RemotePort, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回更新后的隧道
	var updatedTunnel Tunnel
	var dbID, dbDeviceID int
	err = db.QueryRow("SELECT id, device_id, name, type, local_ip, local_port, remote_port, status, created_at, updated_at FROM tunnels WHERE id = ?", id).Scan(
		&dbID, &dbDeviceID, &updatedTunnel.Name, &updatedTunnel.Type, &updatedTunnel.LocalIP, &updatedTunnel.LocalPort, &updatedTunnel.RemotePort, &updatedTunnel.Status, &updatedTunnel.CreatedAt, &updatedTunnel.UpdatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 转换为uint
	updatedTunnel.ID = uint(dbID)
	updatedTunnel.DeviceID = uint(dbDeviceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedTunnel)
}

// 删除隧道
func deleteTunnel(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// 获取隧道ID
	var tunnelID uint
	err := db.QueryRow("SELECT id FROM tunnels WHERE id = ?", id).Scan(&tunnelID)
	if err == nil {
		// 停止隧道（如果正在运行）
		tunnelManager.StopTunnel(tunnelID)
	}

	// 删除隧道记录
	_, err = db.Exec("DELETE FROM tunnels WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// 更新隧道状态
func updateTunnelStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 验证状态值
	if req.Status != "active" && req.Status != "inactive" {
		http.Error(w, "Invalid status. Must be 'active' or 'inactive'", http.StatusBadRequest)
		return
	}

	// 获取隧道当前信息
	var tunnel Tunnel
	var dbID, dbDeviceID int
	err := db.QueryRow("SELECT id, device_id, name, type, local_ip, local_port, remote_port FROM tunnels WHERE id = ?", id).Scan(
		&dbID, &dbDeviceID, &tunnel.Name, &tunnel.Type, &tunnel.LocalIP, &tunnel.LocalPort, &tunnel.RemotePort)
	if err != nil {
		http.Error(w, "Tunnel not found", http.StatusNotFound)
		return
	}

	// 转换为uint
	tunnel.ID = uint(dbID)
	tunnel.DeviceID = uint(dbDeviceID)

	// 更新隧道状态
	_, err = db.Exec(`
		UPDATE tunnels SET status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Status, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 根据状态启动或停止隧道
	if req.Status == "active" {
		// 启动隧道
		err = tunnelManager.StartTunnel(tunnel.ID, tunnel.DeviceID, tunnel.Name, tunnel.Type, tunnel.LocalIP, tunnel.LocalPort, tunnel.RemotePort)
		if err != nil {
			log.Printf("Failed to start tunnel %d: %v", tunnel.ID, err)
		}
	} else {
		// 停止隧道
		err = tunnelManager.StopTunnel(tunnel.ID)
		if err != nil {
			log.Printf("Failed to stop tunnel %d: %v", tunnel.ID, err)
		}
	}

	// 返回更新后的隧道
	var updatedTunnel Tunnel
	var updatedDBID, updatedDBDeviceID int
	err = db.QueryRow("SELECT id, device_id, name, type, local_ip, local_port, remote_port, status, created_at, updated_at FROM tunnels WHERE id = ?", id).Scan(
		&updatedDBID, &updatedDBDeviceID, &updatedTunnel.Name, &updatedTunnel.Type, &updatedTunnel.LocalIP, &updatedTunnel.LocalPort, &updatedTunnel.RemotePort, &updatedTunnel.Status, &updatedTunnel.CreatedAt, &updatedTunnel.UpdatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 转换为uint
	updatedTunnel.ID = uint(updatedDBID)
	updatedTunnel.DeviceID = uint(updatedDBDeviceID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedTunnel)
}

// 根据设备ID获取隧道列表
func getTunnelsByDevice(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deviceID := vars["deviceId"]

	rows, err := db.Query("SELECT id, device_id, name, type, local_ip, local_port, remote_port, status, created_at, updated_at FROM tunnels WHERE device_id = ? ORDER BY created_at DESC", deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tunnels []Tunnel
	for rows.Next() {
		var t Tunnel
		var id, deviceID int
		err := rows.Scan(&id, &deviceID, &t.Name, &t.Type, &t.LocalIP, &t.LocalPort, &t.RemotePort, &t.Status, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t.ID = uint(id)
		t.DeviceID = uint(deviceID)
		tunnels = append(tunnels, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":  200,
		"msg":   "",
		"count": len(tunnels),
		"data":  tunnels,
	})
}

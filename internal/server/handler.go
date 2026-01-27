package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"tbore/internal/common"
	"tbore/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocketConnection WebSocket连接信息
type WebSocketConnection struct {
	Conn      *websocket.Conn
	DeviceID  uint
	AgentID   string
	CreatedAt time.Time
}

// WebTerminalInfo Web终端连接信息
type WebTerminalInfo struct {
	Conn       *websocket.Conn
	DeviceID   uint
	TerminalID byte
}

// Handler API处理器
type Handler struct {
	tokenService   *TokenService
	deviceService  *DeviceService
	tagService     *TagService
	tunnelService  *TunnelService
	accountService *AccountService
	tunnelManager  *TunnelManager
	// WebSocket升级器
	upgrader websocket.Upgrader
	// WebSocket连接池（Agent连接）
	wsConnections map[string]*WebSocketConnection
	// Web终端连接映射，key格式：设备ID-终端ID
	webTermConnections map[string]*WebTerminalInfo
	wsMutex            sync.Mutex
}

// NewHandler 创建API处理器实例
func NewHandler() *Handler {
	tunnelService := NewTunnelService()
	return &Handler{
		tokenService:   NewTokenService(),
		deviceService:  NewDeviceService(),
		tagService:     NewTagService(),
		tunnelService:  tunnelService,
		accountService: NewAccountService(),
		tunnelManager:  NewTunnelManager(tunnelService),
		// 初始化WebSocket升级器
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// 允许所有来源
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		// 初始化WebSocket连接池（Agent连接）
		wsConnections: make(map[string]*WebSocketConnection),
		// 初始化Web终端连接映射
		webTermConnections: make(map[string]*WebTerminalInfo),
	}
}

// RegisterRoutes 注册API路由
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	// API版本前缀
	api := router.Group("/api/v1")

	// Token管理路由
	tokens := api.Group("/tokens")
	{
		tokens.POST("", h.createToken)
		tokens.GET("", h.getAllTokens)
		tokens.GET("/:id", h.getToken)
		tokens.PUT("/:id", h.updateToken)
		tokens.DELETE("/:id", h.deleteToken)
		tokens.POST("/validate", h.validateToken)
	}

	// 标签管理路由
	tags := api.Group("/tags")
	{
		tags.POST("", h.createTag)
		tags.GET("", h.getAllTags)
		tags.GET("/:id", h.getTag)
		tags.PUT("/:id", h.updateTag)
		tags.DELETE("/:id", h.deleteTag)
	}

	// 设备管理路由
	devices := api.Group("/devices")
	{
		devices.POST("/register", h.registerDevice)
		devices.POST("/heartbeat", h.updateHeartbeat)
		devices.GET("", h.getAllDevices)
		devices.GET("/:id", h.getDevice)
		devices.PUT("/:id", h.updateDevice)
		devices.GET("/:id/tags", h.getDeviceTags)
		devices.POST("/:id/tags", h.addDeviceTag)
		devices.DELETE("/:id/tags/:tagId", h.removeDeviceTag)
		// Web终端路由
		devices.GET("/:id/webterm", h.webTerminal)
	}

	// 隧道管理路由
	tunnels := api.Group("/tunnels")
	{
		tunnels.POST("", h.createTunnel)
		tunnels.PUT("/:id/status", h.updateTunnelStatus)
		tunnels.GET("/device/:deviceId", h.getTunnelsByDevice)
		tunnels.GET("", h.getAllTunnels)
		tunnels.DELETE("/:id", h.deleteTunnel)
	}

	// 账号管理路由
	accounts := api.Group("/accounts")
	{
		accounts.POST("", h.createAccount)
		accounts.GET("", h.getAllAccounts)
		accounts.GET("/:id", h.getAccount)
		accounts.PUT("/:id", h.updateAccount)
		accounts.DELETE("/:id", h.deleteAccount)
		accounts.PUT("/:id/status", h.toggleAccountStatus)
		// 账号设备关联路由
		accounts.POST("/:id/devices", h.bindDeviceToAccount)
		accounts.DELETE("/:id/devices/:device_id", h.unbindDeviceFromAccount)
	}

	// Agent隧道配置API
	api.GET("/agent/tunnels/:agentId", h.getAgentTunnels)
	api.POST("/agent/tunnels/:agentId/status", h.updateAgentTunnelStatus)

	// WebSocket路由
	router.GET("/ws/agent", h.handleAgentWebSocket)
}

// ---------------- Token 相关API ----------------

// @Summary 创建新Token
// @Description 生成一个新的API Token用于Agent注册
// @Tags Token管理
// @Accept json
// @Produce json
// @Param token body struct{Name string;Description string} true "Token信息"
// @Success 200 {object} model.Token
// @Router /api/v1/tokens [post]
func (h *Handler) createToken(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, err := h.tokenService.GenerateToken(req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, token)
}

// @Summary 获取所有Token
// @Description 获取所有已生成的Token列表
// @Tags Token管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/tokens [get]
func (h *Handler) getAllTokens(c *gin.Context) {
	tokens, err := h.tokenService.GetAllTokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Token{}})
		return
	}

	// 为每个Token添加使用主机数
	// 创建一个新的切片来存储带有使用主机数的Token
	type TokenWithHosts struct {
		model.Token
		UsedHosts int64 `json:"used_hosts"`
	}

	tokensWithHosts := make([]TokenWithHosts, len(tokens))
	for i, token := range tokens {
		var count int64
		common.DB.Model(&model.Device{}).Where("token_id = ?", token.ID).Count(&count)
		// 使用新结构体扩展Token，添加used_hosts字段
		tokensWithHosts[i] = TokenWithHosts{
			Token:     token,
			UsedHosts: count,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(tokens),
		"data":  tokensWithHosts,
	})
}

// @Summary 获取单个Token
// @Description 根据ID获取Token详情
// @Tags Token管理
// @Produce json
// @Param id path uint true "Token ID"
// @Success 200 {object} model.Token
// @Router /api/v1/tokens/{id} [get]
func (h *Handler) getToken(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	token, err := h.tokenService.GetTokenByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Token not found"})
		return
	}

	c.JSON(http.StatusOK, token)
}

// @Summary 更新Token
// @Description 更新Token信息
// @Tags Token管理
// @Accept json
// @Produce json
// @Param id path uint true "Token ID"
// @Param token body struct{Name string;Description string;Status string} true "Token信息"
// @Success 200 {object} model.Token
// @Router /api/v1/tokens/{id} [put]
func (h *Handler) updateToken(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Status      string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, err := h.tokenService.UpdateToken(uint(id), req.Name, req.Description, req.Status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, token)
}

// @Summary 删除Token
// @Description 删除指定ID的Token
// @Tags Token管理
// @Param id path uint true "Token ID"
// @Success 204 {object} nil
// @Router /api/v1/tokens/{id} [delete]
func (h *Handler) deleteToken(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID"})
		return
	}

	if err := h.tokenService.DeleteToken(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary 验证Token
// @Description 验证Token是否有效
// @Tags Token管理
// @Accept json
// @Produce json
// @Param token body struct{Token string} true "Token值"
// @Success 200 {object} struct{Valid bool}
// @Router /api/v1/tokens/validate [post]
func (h *Handler) validateToken(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	valid, err := h.tokenService.ValidateToken(req.Token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"valid": valid})
}

// ---------------- 设备相关API ----------------

// @Summary 注册设备
// @Description Agent注册新设备
// @Tags 设备管理
// @Accept json
// @Produce json
// @Param device body struct{AgentID string;IPAddress string;OS string;Token string;Version string;SystemInfo model.SystemInfo;NetworkInfos []model.NetworkInfo;DiskMounts []model.DiskMount} true "设备注册信息"
// @Success 200 {object} model.Device
// @Router /api/v1/devices/register [post]
func (h *Handler) registerDevice(c *gin.Context) {
	var req struct {
		AgentID      string              `json:"agent_id" binding:"required"`
		IPAddress    string              `json:"ip_address" binding:"required"`
		OS           string              `json:"os" binding:"required"`
		Version      string              `json:"version"`
		SystemInfo   model.SystemInfo    `json:"system_info"`
		NetworkInfos []model.NetworkInfo `json:"network_infos"`
		DiskMounts   []model.DiskMount   `json:"disk_mounts"`
		Token        string              `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 验证Token有效性并获取TokenID
	token, err := h.tokenService.GetTokenByValue(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}

	if token.Status != "active" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Token is not active"})
		return
	}

	device, err := h.deviceService.RegisterDevice(req.AgentID, req.IPAddress, req.OS, req.Version, token.ID, req.SystemInfo, req.NetworkInfos, req.DiskMounts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, device)
}

// @Summary 更新设备心跳
// @Description 更新设备心跳状态
// @Tags 设备管理
// @Accept json
// @Produce json
// @Param heartbeat body struct{AgentID string} true "心跳信息"
// @Success 200 {object} struct{Success bool}
// @Router /api/v1/devices/heartbeat [post]
func (h *Handler) updateHeartbeat(c *gin.Context) {
	var req struct {
		AgentID string `json:"agent_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.deviceService.UpdateDeviceHeartbeat(req.AgentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// @Summary 获取所有设备
// @Description 获取所有设备列表
// @Tags 设备管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/devices [get]
func (h *Handler) getAllDevices(c *gin.Context) {
	devices, err := h.deviceService.GetAllDevices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Device{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(devices),
		"data":  devices,
	})
}

// @Summary 获取单个设备
// @Description 根据ID获取设备详情
// @Tags 设备管理
// @Produce json
// @Param id path uint true "设备ID"
// @Success 200 {object} model.Device
// @Router /api/v1/devices/{id} [get]
func (h *Handler) getDevice(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	device, err := h.deviceService.GetDeviceByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	c.JSON(http.StatusOK, device)
}

// @Summary 更新设备信息
// @Description 更新设备基本信息
// @Tags 设备管理
// @Accept json
// @Produce json
// @Param id path uint true "设备ID"
// @Param device body struct{Name string} true "设备信息"
// @Success 200 {object} model.Device
// @Router /api/v1/devices/{id} [put]
func (h *Handler) updateDevice(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	device, err := h.deviceService.UpdateDevice(uint(id), req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, device)
}

// ---------------- Web终端相关API ----------------

// @Summary Web终端连接
// @Description 建立Web终端WebSocket连接
// @Tags 终端管理
// @Param deviceId path uint true "设备ID"
// @Param username query string true "SSH用户名"
// @Param password query string true "SSH密码"
// @Param port query int true "SSH端口"
// @Param cols query int true "终端列数"
// @Param rows query int true "终端行数"
// @Success 101 {string} string "Switching Protocols"
// @Router /api/v1/devices/{deviceId}/webterm [get]
func (h *Handler) webTerminal(c *gin.Context) {
	// 升级HTTP连接为WebSocket连接
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upgrade to WebSocket: " + err.Error()})
		return
	}

	// 获取设备ID
	deviceIDStr := c.Param("id")
	deviceID, err := strconv.ParseUint(deviceIDStr, 10, 32)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("无效的设备ID\r\n"))
		conn.Close()
		return
	}

	// 获取设备信息
	device, err := h.deviceService.GetDeviceByID(uint(deviceID))
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("设备不存在\r\n"))
		conn.Close()
		return
	}

	// 检查设备是否有活跃的WebSocket连接
	h.wsMutex.Lock()
	agentWSConn, exists := h.wsConnections[device.AgentID]
	h.wsMutex.Unlock()

	if !exists {
		conn.WriteMessage(websocket.TextMessage, []byte("设备未连接到服务器，无法建立Web终端\r\n"))
		conn.Close()
		return
	}

	// 获取SSH连接参数
	username := c.Query("username")
	password := c.Query("password")
	portStr := c.Query("port")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		port = 22 // 默认SSH端口
	}

	// 获取终端尺寸参数
	colsStr := c.Query("cols")
	rowsStr := c.Query("rows")
	cols, err := strconv.Atoi(colsStr)
	if err != nil || cols <= 0 {
		cols = 80 // 默认列数
	}
	rows, err := strconv.Atoi(rowsStr)
	if err != nil || rows <= 0 {
		rows = 24 // 默认行数
	}

	// 生成唯一的终端ID
	terminalID := byte(1) // 简单起见，使用固定ID，后续可以改进为动态分配

	// 构建web终端连接键
	connKey := fmt.Sprintf("%d-%d", deviceID, terminalID)

	// 保存web终端连接信息
	h.wsMutex.Lock()
	h.webTermConnections[connKey] = &WebTerminalInfo{
		Conn:       conn,
		DeviceID:   uint(deviceID),
		TerminalID: terminalID,
	}
	h.wsMutex.Unlock()

	// 构建SSH连接请求
	sshReq := struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Port     int    `json:"port"`
		Cols     int    `json:"cols"`
		Rows     int    `json:"rows"`
	}{
		Username: username,
		Password: password,
		Port:     port,
		Cols:     cols,
		Rows:     rows,
	}

	sshReqBytes, err := json.Marshal(sshReq)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("构建SSH请求失败: "+err.Error()+"\r\n"))
		return
	}

	// 构建WebSocket消息: [terminalID][opCode=0x00][data]
	msg := make([]byte, 2+len(sshReqBytes))
	msg[0] = terminalID
	msg[1] = 0x00 // 0x00=SSH连接请求
	copy(msg[2:], sshReqBytes)

	// 发送SSH连接请求到Agent
	if err := agentWSConn.Conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("发送SSH连接请求失败: "+err.Error()+"\r\n"))
		return
	}

	conn.WriteMessage(websocket.TextMessage, []byte("正在通过WebSocket隧道建立SSH连接...\r\n"))

	// 数据转发上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// 移除从Agent WebSocket读取数据的goroutine，改为通过handleAgentWebSocket函数的主循环处理
	// 这样可以确保每个WebSocket连接只有一个goroutine在读取数据，避免协议帧混乱

	// 从客户端WebSocket读取数据，发送到Agent WebSocket
	wg.Add(1)
	go func() {
		defer func() {
			// 客户端断开连接，发送断开连接请求到Agent
			disconnectMsg := []byte{terminalID, 0x02} // 0x02=SSH断开连接请求
			agentWSConn.Conn.WriteMessage(websocket.BinaryMessage, disconnectMsg)
			wg.Done()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				messageType, p, err := conn.ReadMessage()
				if err != nil {
					return
				}

				// 只处理文本消息，因为终端输入应该是文本数据
				if messageType == websocket.TextMessage {
					// 构建消息：[terminalID][opCode=0x01][data]
					msg := make([]byte, 2+len(p))
					msg[0] = terminalID
					msg[1] = 0x01 // 0x01=SSH数据
					copy(msg[2:], p)

					// 发送数据到Agent WebSocket
					h.wsMutex.Lock()
					// 检查Agent连接是否仍然存在
					if agentConn, exists := h.wsConnections[device.AgentID]; exists {
						if err := agentConn.Conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
							conn.WriteMessage(websocket.TextMessage, []byte("发送数据到设备失败: "+err.Error()+"\r\n"))
							// 关闭连接
							conn.Close()
							h.wsMutex.Unlock()
							return
						}
					}
					h.wsMutex.Unlock()
				}
			}
		}
	}()

	// 等待所有goroutine完成
	wg.Wait()

	// 移除连接映射
	h.wsMutex.Lock()
	delete(h.webTermConnections, connKey)
	h.wsMutex.Unlock()

	// 关闭连接
	conn.Close()
}

// ---------------- 标签相关API ----------------

// @Summary 创建标签
// @Description 创建新标签
// @Tags 标签管理
// @Accept json
// @Produce json
// @Param tag body struct{Name string;Description string} true "标签信息"
// @Success 200 {object} model.Tag
// @Router /api/v1/tags [post]
func (h *Handler) createTag(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tag, err := h.tagService.CreateTag(req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tag)
}

// @Summary 获取所有标签
// @Description 获取所有标签列表
// @Tags 标签管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/tags [get]
func (h *Handler) getAllTags(c *gin.Context) {
	tags, err := h.tagService.GetAllTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Tag{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(tags),
		"data":  tags,
	})
}

// @Summary 获取单个标签
// @Description 根据ID获取标签详情
// @Tags 标签管理
// @Produce json
// @Param id path uint true "标签ID"
// @Success 200 {object} model.Tag
// @Router /api/v1/tags/{id} [get]
func (h *Handler) getTag(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	tag, err := h.tagService.GetTagByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
		return
	}

	c.JSON(http.StatusOK, tag)
}

// @Summary 更新标签
// @Description 更新标签信息
// @Tags 标签管理
// @Accept json
// @Produce json
// @Param id path uint true "标签ID"
// @Param tag body struct{Name string;Description string} true "标签信息"
// @Success 200 {object} model.Tag
// @Router /api/v1/tags/{id} [put]
func (h *Handler) updateTag(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tag, err := h.tagService.UpdateTag(uint(id), req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tag)
}

// @Summary 删除标签
// @Description 删除指定ID的标签
// @Tags 标签管理
// @Param id path uint true "标签ID"
// @Success 204 {object} nil
// @Router /api/v1/tags/{id} [delete]
func (h *Handler) deleteTag(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	if err := h.tagService.DeleteTag(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary 获取设备标签
// @Description 获取设备的所有标签
// @Tags 设备管理
// @Produce json
// @Param id path uint true "设备ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/devices/{id}/tags [get]
func (h *Handler) getDeviceTags(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid device ID", "count": 0, "data": []model.Tag{}})
		return
	}

	tags, err := h.tagService.GetDeviceTags(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Tag{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(tags),
		"data":  tags,
	})
}

// @Summary 添加设备标签
// @Description 给设备添加标签
// @Tags 设备管理
// @Accept json
// @Produce json
// @Param id path uint true "设备ID"
// @Param tag body struct{TagID uint} true "标签ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/devices/{id}/tags [post]
func (h *Handler) addDeviceTag(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	var req struct {
		TagID uint `json:"tag_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.tagService.AddTagToDevice(uint(id), req.TagID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// @Summary 移除设备标签
// @Description 从设备移除标签
// @Tags 设备管理
// @Param id path uint true "设备ID"
// @Param tagId path uint true "标签ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/devices/{id}/tags/{tagId} [delete]
func (h *Handler) removeDeviceTag(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	tagIdStr := c.Param("tagId")
	tagId, err := strconv.ParseUint(tagIdStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
		return
	}

	if err := h.tagService.RemoveTagFromDevice(uint(id), uint(tagId)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ---------------- 隧道相关API ----------------

// @Summary 创建隧道
// @Description 创建新的端口转发隧道
// @Tags 隧道管理
// @Accept json
// @Produce json
// @Param tunnel body struct{DeviceID uint;Name string;Type string;LocalIP string;LocalPort int;RemotePort int} true "隧道信息"
// @Success 200 {object} model.Tunnel
// @Router /api/v1/tunnels [post]
func (h *Handler) createTunnel(c *gin.Context) {
	var req struct {
		DeviceID   uint   `json:"device_id" binding:"required"`
		Name       string `json:"name" binding:"required"`
		Type       string `json:"type" binding:"required"`
		LocalIP    string `json:"local_ip" binding:"required"`
		LocalPort  int    `json:"local_port" binding:"required"`
		RemotePort int    `json:"remote_port" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tunnel, err := h.tunnelService.CreateTunnel(req.DeviceID, req.Name, req.Type, req.LocalIP, req.LocalPort, req.RemotePort)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if tunnel == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Remote port already in use"})
		return
	}

	// 启动隧道
	if err := h.tunnelManager.StartTunnel(tunnel.ID, tunnel.DeviceID, tunnel.Name, tunnel.Type, tunnel.LocalIP, tunnel.LocalPort, tunnel.RemotePort); err != nil {
		// 隧道启动失败，更新隧道状态为inactive
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start tunnel: " + err.Error()})
		return
	}

	// 更新隧道状态为active
	if err := h.tunnelService.UpdateTunnelStatus(tunnel.ID, "active"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tunnel status: " + err.Error()})
		return
	}

	// 更新隧道对象的状态
	tunnel.Status = "active"

	c.JSON(http.StatusOK, tunnel)
}

// @Summary 更新隧道状态
// @Description 更新隧道状态
// @Tags 隧道管理
// @Accept json
// @Produce json
// @Param id path uint true "隧道ID"
// @Param status body struct{Status string} true "状态信息"
// @Success 200 {object} struct{Success bool}
// @Router /api/v1/tunnels/{id}/status [put]
func (h *Handler) updateTunnelStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新隧道状态
	if err := h.tunnelService.UpdateTunnelStatus(uint(id), req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 根据状态更新隧道管理器
	if req.Status == "active" {
		// 获取隧道信息
		tunnel, err := h.tunnelService.GetTunnelByID(uint(id))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get tunnel: " + err.Error()})
			return
		}
		// 启动隧道
		if err := h.tunnelManager.StartTunnel(tunnel.ID, tunnel.DeviceID, tunnel.Name, tunnel.Type, tunnel.LocalIP, tunnel.LocalPort, tunnel.RemotePort); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start tunnel: " + err.Error()})
			return
		}
	} else {
		// 停止隧道
		h.tunnelManager.StopTunnel(uint(id))
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// @Summary 获取设备隧道列表
// @Description 根据设备ID获取隧道列表
// @Tags 隧道管理
// @Produce json
// @Param deviceId path uint true "设备ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/tunnels/device/{deviceId} [get]
func (h *Handler) getTunnelsByDevice(c *gin.Context) {
	deviceIDStr := c.Param("deviceId")
	deviceID, err := strconv.ParseUint(deviceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid device ID", "count": 0, "data": []model.Tunnel{}})
		return
	}

	tunnels, err := h.tunnelService.GetTunnelsByDeviceID(uint(deviceID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Tunnel{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(tunnels),
		"data":  tunnels,
	})
}

// @Summary 获取所有隧道
// @Description 获取所有隧道列表
// @Tags 隧道管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/tunnels [get]
func (h *Handler) getAllTunnels(c *gin.Context) {
	// 从数据库获取所有隧道
	tunnels, err := h.tunnelService.GetAllTunnels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Tunnel{}})
		return
	}

	// 获取所有隧道的实时统计信息
	realTimeStats := h.tunnelManager.GetAllTunnelStats()
	statsMap := make(map[uint]*TunnelStats)
	for _, stats := range realTimeStats {
		statsMap[stats.TunnelID] = stats
	}

	// 合并实时统计信息到隧道对象
	for i, tunnel := range tunnels {
		if stats, exists := statsMap[tunnel.ID]; exists {
			// 更新实时统计信息
			tunnels[i].ConnectionCount = stats.ConnectionCount
			tunnels[i].TotalConnections = stats.TotalConnections
			tunnels[i].BytesSent = stats.BytesSent
			tunnels[i].BytesRecv = stats.BytesRecv
			tunnels[i].LastActive = stats.LastActive
			tunnels[i].Status = stats.Status
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(tunnels),
		"data":  tunnels,
	})
}

// @Summary 删除隧道
// @Description 删除指定ID的隧道
// @Tags 隧道管理
// @Param id path uint true "隧道ID"
// @Success 204 {object} nil
// @Router /api/v1/tunnels/{id} [delete]
func (h *Handler) deleteTunnel(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tunnel ID"})
		return
	}

	// 停止隧道
	h.tunnelManager.StopTunnel(uint(id))

	// 删除隧道
	if err := h.tunnelService.DeleteTunnel(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary 获取Agent隧道配置
// @Description 根据Agent ID获取隧道配置
// @Tags Agent管理
// @Produce json
// @Param agentId path string true "Agent ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/agent/tunnels/{agentId} [get]
func (h *Handler) getAgentTunnels(c *gin.Context) {
	agentId := c.Param("agentId")

	// 根据Agent ID获取设备
	var device model.Device
	if err := common.DB.Where("agent_id = ?", agentId).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// 获取该设备的所有隧道
	tunnels, err := h.tunnelService.GetTunnelsByDeviceID(device.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为Agent需要的隧道配置格式
	type AgentTunnelConfig struct {
		ID         uint   `json:"id"`
		Name       string `json:"name"`
		LocalIP    string `json:"local_ip"`
		LocalPort  int    `json:"local_port"`
		RemotePort int    `json:"remote_port"`
		Status     string `json:"status"`
	}

	var agentTunnels []AgentTunnelConfig
	for _, tunnel := range tunnels {
		agentTunnels = append(agentTunnels, AgentTunnelConfig{
			ID:         tunnel.ID,
			Name:       tunnel.Name,
			LocalIP:    tunnel.LocalIP,
			LocalPort:  tunnel.LocalPort,
			RemotePort: tunnel.RemotePort,
			Status:     tunnel.Status,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(agentTunnels),
		"data":  agentTunnels,
	})
}

// @Summary 处理Agent WebSocket连接
// @Description 处理Agent的WebSocket长连接
// @Tags Agent管理
// @Success 101 {string} string "Switching Protocols"
// @Router /ws/agent [get]
func (h *Handler) handleAgentWebSocket(c *gin.Context) {
	// 升级HTTP连接为WebSocket连接
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upgrade to WebSocket: " + err.Error()})
		return
	}

	// 获取Agent ID和Token
	agentID := c.GetHeader("Agent-ID")
	token := c.GetHeader("Token")

	if agentID == "" || token == "" {
		conn.WriteMessage(websocket.TextMessage, []byte("Missing Agent-ID or Token header"))
		conn.Close()
		return
	}

	// 验证Token有效性
	valid, err := h.tokenService.ValidateToken(token)
	if err != nil || !valid {
		conn.WriteMessage(websocket.TextMessage, []byte("Invalid token"))
		conn.Close()
		return
	}

	// 根据Agent ID获取设备
	var device model.Device
	if err := common.DB.Where("agent_id = ?", agentID).First(&device).Error; err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Device not found"))
		conn.Close()
		return
	}

	// 创建WebSocket连接信息
	wsConn := &WebSocketConnection{
		Conn:      conn,
		DeviceID:  device.ID,
		AgentID:   agentID,
		CreatedAt: time.Now(),
	}

	// 将连接添加到连接池
	h.wsMutex.Lock()
	h.wsConnections[agentID] = wsConn
	h.wsMutex.Unlock()

	log.Printf("Agent %s connected via WebSocket, device ID: %d", agentID, device.ID)

	// 更新设备心跳
	if err := h.deviceService.UpdateDeviceHeartbeat(agentID); err != nil {
		log.Printf("Failed to update device heartbeat: %v", err)
	}

	// 处理WebSocket消息
	defer func() {
		// 从连接池移除
		h.wsMutex.Lock()
		delete(h.wsConnections, agentID)
		h.wsMutex.Unlock()

		conn.Close()
		log.Printf("Agent %s disconnected from WebSocket", agentID)
	}()

	// 消息处理循环
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			// 连接关闭
			break
		}

		// 处理消息
		switch messageType {
		case websocket.TextMessage:
			// 处理文本消息（暂时忽略）
			log.Printf("Received text message from agent %s: %s", agentID, string(message))
		case websocket.BinaryMessage:
			// 处理二进制消息（SSH数据转发）
			log.Printf("Received binary message from agent %s: %d bytes", agentID, len(message))

			// 检查消息长度
			if len(message) < 2 {
				log.Printf("Invalid binary message: too short")
				continue
			}

			// 获取终端ID和操作码
			terminalID := message[0]
			opCode := message[1]
			data := message[2:]

			// 根据AgentID获取设备ID
			var device model.Device
			if err := common.DB.Where("agent_id = ?", agentID).First(&device).Error; err != nil {
				log.Printf("Failed to get device for agent %s: %v", agentID, err)
				continue
			}

			// 构建web终端连接键
			connKey := fmt.Sprintf("%d-%d", device.ID, terminalID)

			// 查找对应的web终端连接
			h.wsMutex.Lock()
			webTermInfo, exists := h.webTermConnections[connKey]
			h.wsMutex.Unlock()

			if !exists {
				// 如果没有找到对应的web终端连接，可能是web终端已经关闭
				// 只有当收到的不是断开连接通知时，才发送断开连接请求
				if opCode != 0x05 {
					disconnectMsg := []byte{terminalID, 0x02} // 0x02=SSH断开连接请求
					if err := conn.WriteMessage(websocket.BinaryMessage, disconnectMsg); err != nil {
						log.Printf("Failed to send disconnect message: %v", err)
					}
				}
				continue
			}

			// 根据操作码处理消息 - 只处理已知的操作码
			switch opCode {
			case 0x04: // SSH连接成功
				if err := webTermInfo.Conn.WriteMessage(websocket.TextMessage, []byte("SSH连接成功，开始会话...\r\n")); err != nil {
					log.Printf("Failed to send SSH success message: %v", err)
				}
			case 0x01: // SSH数据
				// 将SSH数据发送到客户端WebSocket，使用二进制消息格式以避免协议错误
				if len(data) > 0 {
					if err := webTermInfo.Conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
						log.Printf("Failed to send SSH data to client: %v", err)
						// 移除无效的web终端连接
						h.wsMutex.Lock()
						delete(h.webTermConnections, connKey)
						h.wsMutex.Unlock()
						// 发送断开连接请求到Agent
						disconnectMsg := []byte{terminalID, 0x02} // 0x02=SSH断开连接请求
						if err := conn.WriteMessage(websocket.BinaryMessage, disconnectMsg); err != nil {
							log.Printf("Failed to send disconnect message: %v", err)
						}
					}
				}
			case 0x03: // SSH错误
				if len(data) > 0 {
					errCode := data[0]
					errMsg := "SSH连接失败"
					switch errCode {
					case 0x01:
						errMsg = "SSH连接请求解析失败"
					case 0x02:
						errMsg = "无法连接到本地SSH服务器或密码错误"
					case 0x03:
						errMsg = "创建SSH会话失败"
					case 0x04:
						errMsg = "请求PTY失败"
					case 0x05:
						errMsg = "写入SSH数据失败"
					}
					if err := webTermInfo.Conn.WriteMessage(websocket.TextMessage, []byte(errMsg+"\r\n")); err != nil {
						log.Printf("Failed to send SSH error message: %v", err)
					}
					// 关闭连接，避免无限等待
					webTermInfo.Conn.Close()
					// 移除无效的web终端连接
					h.wsMutex.Lock()
					delete(h.webTermConnections, connKey)
					h.wsMutex.Unlock()
				}
			case 0x05: // SSH断开连接
				if err := webTermInfo.Conn.WriteMessage(websocket.TextMessage, []byte("\r\nSSH会话已断开\r\n")); err != nil {
					log.Printf("Failed to send SSH disconnect message: %v", err)
				}
				webTermInfo.Conn.Close()
				// 移除无效的web终端连接
				h.wsMutex.Lock()
				delete(h.webTermConnections, connKey)
				h.wsMutex.Unlock()
			default:
				log.Printf("Unknown opCode: %d", opCode)
				// 只记录日志，不做其他处理，避免协议错误
			}
		case websocket.PingMessage:
			// 回复pong
			if err := conn.WriteMessage(websocket.PongMessage, nil); err != nil {
				log.Printf("Failed to send pong: %v", err)
				break
			}
		}
	}
}

// @Summary 更新Agent隧道状态
// @Description 更新Agent隧道状态
// @Tags Agent管理
// @Accept json
// @Produce json
// @Param agentId path string true "Agent ID"
// @Param status body struct{TunnelID uint;Status string} true "隧道状态信息"
// @Success 200 {object} struct{Success bool}
// @Router /api/v1/agent/tunnels/{agentId}/status [post]
func (h *Handler) updateAgentTunnelStatus(c *gin.Context) {
	agentId := c.Param("agentId")

	var req struct {
		TunnelID uint   `json:"tunnel_id" binding:"required"`
		Status   string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 根据Agent ID获取设备
	var device model.Device
	if err := common.DB.Where("agent_id = ?", agentId).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// 检查隧道是否属于该设备
	var tunnel model.Tunnel
	if err := common.DB.Where("id = ? AND device_id = ?", req.TunnelID, device.ID).First(&tunnel).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "Tunnel not found or not belongs to this device"})
		return
	}

	// 更新隧道状态
	if err := h.tunnelService.UpdateTunnelStatus(req.TunnelID, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// @Summary 创建账号
// @Description 创建新账号
// @Tags 账号管理
// @Accept json
// @Produce json
// @Param account body model.Account true "账号信息"
// @Success 200 {object} model.Account
// @Router /api/v1/accounts [post]
func (h *Handler) createAccount(c *gin.Context) {
	var account model.Account

	if err := c.ShouldBindJSON(&account); err != nil {
		log.Printf("CreateAccount bind error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": 400})
		return
	}

	log.Printf("CreateAccount bind success: %+v", account)

	createdAccount, err := h.accountService.CreateAccount(
		account.Name,
		account.Username,
		account.AuthType,
		account.Password,
		account.Description,
		account.IsActive,
		account.IsPrivileged,
	)
	if err != nil {
		log.Printf("CreateAccount service error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": 500})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Account created successfully",
		"data": createdAccount,
	})
}

// @Summary 获取所有账号
// @Description 获取所有账号列表
// @Tags 账号管理
// @Produce json
// @Success 200 {object} []model.Account
// @Router /api/v1/accounts [get]
func (h *Handler) getAllAccounts(c *gin.Context) {
	accounts, err := h.accountService.GetAllAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error(), "count": 0, "data": []model.Account{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"msg":   "",
		"count": len(accounts),
		"data":  accounts,
	})
}

// @Summary 获取单个账号
// @Description 根据ID获取账号详情
// @Tags 账号管理
// @Produce json
// @Param id path uint true "账号ID"
// @Success 200 {object} model.Account
// @Router /api/v1/accounts/{id} [get]
func (h *Handler) getAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	account, err := h.accountService.GetAccountByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "",
		"data": account,
	})
}

// @Summary 更新账号
// @Description 更新账号信息
// @Tags 账号管理
// @Accept json
// @Produce json
// @Param id path uint true "账号ID"
// @Param account body map[string]interface{} true "账号信息（只更新提供的字段）"
// @Success 200 {object} model.Account
// @Router /api/v1/accounts/{id} [put]
func (h *Handler) updateAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	// 先获取原账号信息
	var account model.Account
	if err := common.DB.Where("id = ?", uint(id)).First(&account).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	// 绑定请求体到map
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 只更新提供的字段
	if name, ok := updates["name"].(string); ok {
		account.Name = name
	}
	if username, ok := updates["username"].(string); ok {
		account.Username = username
	}
	if authType, ok := updates["auth_type"].(string); ok {
		account.AuthType = authType
	}
	if password, ok := updates["password"].(string); ok {
		account.Password = password
	}
	if description, ok := updates["description"].(string); ok {
		account.Description = description
	}
	if isActive, ok := updates["is_active"].(bool); ok {
		account.IsActive = isActive
	}
	if isPrivileged, ok := updates["is_privileged"].(bool); ok {
		account.IsPrivileged = isPrivileged
	}

	account.UpdatedAt = time.Now()

	if err := common.DB.Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Account updated successfully",
		"data": account,
	})
}

// @Summary 绑定设备到账号
// @Description 将设备绑定到账号
// @Tags 账号管理
// @Accept json
// @Produce json
// @Param id path uint true "账号ID"
// @Param device_id body struct{DeviceID uint} true "设备ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/accounts/{id}/devices [post]
func (h *Handler) bindDeviceToAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	var req struct {
		DeviceID uint `json:"device_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.accountService.BindDeviceToAccount(uint(id), req.DeviceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Device bound to account successfully",
	})
}

// @Summary 从账号解绑设备
// @Description 将设备从账号解绑
// @Tags 账号管理
// @Produce json
// @Param id path uint true "账号ID"
// @Param device_id path uint true "设备ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/accounts/{id}/devices/{device_id} [delete]
func (h *Handler) unbindDeviceFromAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	deviceIDStr := c.Param("device_id")
	deviceID, err := strconv.ParseUint(deviceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid device ID"})
		return
	}

	if err := h.accountService.UnbindDeviceFromAccount(uint(id), uint(deviceID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Device unbound from account successfully",
	})
}

// @Summary 删除账号
// @Description 删除指定账号
// @Tags 账号管理
// @Produce json
// @Param id path uint true "账号ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/accounts/{id} [delete]
func (h *Handler) deleteAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	if err := h.accountService.DeleteAccount(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Account deleted successfully",
	})
}

// @Summary 切换账号状态
// @Description 切换账号的激活状态
// @Tags 账号管理
// @Produce json
// @Param id path uint true "账号ID"
// @Success 200 {object} model.Account
// @Router /api/v1/accounts/{id}/status [put]
func (h *Handler) toggleAccountStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	updatedAccount, err := h.accountService.ToggleAccountStatus(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Account status updated successfully",
		"data": updatedAccount,
	})
}

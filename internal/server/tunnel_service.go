package server

import (
	"time"

	"tbore/internal/common"
	"tbore/internal/model"
)

// TunnelService 隧道管理服务
type TunnelService struct{}

// NewTunnelService 创建隧道服务实例
func NewTunnelService() *TunnelService {
	return &TunnelService{}
}

// CreateTunnel 创建新隧道
func (s *TunnelService) CreateTunnel(deviceID uint, name, tunnelType, localIP string, localPort, remotePort int) (*model.Tunnel, error) {
	// 检查远程端口是否已被使用
	var existingTunnel model.Tunnel
	if err := common.DB.Where("remote_port = ? AND status = ?", remotePort, "active").First(&existingTunnel).Error; err == nil {
		return nil, nil // 端口已被使用
	}

	tunnel := &model.Tunnel{
		DeviceID:   deviceID,
		Name:       name,
		Type:       tunnelType,
		LocalIP:    localIP,
		LocalPort:  localPort,
		RemotePort: remotePort,
		Status:     "inactive", // 默认状态为inactive，需要Agent确认
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := common.DB.Create(tunnel).Error; err != nil {
		return nil, err
	}

	return tunnel, nil
}

// UpdateTunnelStatus 更新隧道状态
func (s *TunnelService) UpdateTunnelStatus(id uint, status string) error {
	result := common.DB.Model(&model.Tunnel{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	})

	return result.Error
}

// GetTunnelsByDeviceID 根据设备ID获取隧道列表
func (s *TunnelService) GetTunnelsByDeviceID(deviceID uint) ([]model.Tunnel, error) {
	var tunnels []model.Tunnel
	if err := common.DB.Where("device_id = ?", deviceID).Order("created_at DESC").Find(&tunnels).Error; err != nil {
		return nil, err
	}

	return tunnels, nil
}

// GetAllTunnels 获取所有隧道列表
func (s *TunnelService) GetAllTunnels() ([]model.Tunnel, error) {
	var tunnels []model.Tunnel
	if err := common.DB.Order("created_at DESC").Find(&tunnels).Error; err != nil {
		return nil, err
	}

	return tunnels, nil
}

// GetTunnelByID 根据ID获取隧道
func (s *TunnelService) GetTunnelByID(id uint) (*model.Tunnel, error) {
	var tunnel model.Tunnel
	if err := common.DB.First(&tunnel, id).Error; err != nil {
		return nil, err
	}

	return &tunnel, nil
}

// DeleteTunnel 删除隧道
func (s *TunnelService) DeleteTunnel(id uint) error {
	// 先更新状态为inactive
	if err := s.UpdateTunnelStatus(id, "inactive"); err != nil {
		return err
	}

	// 再删除隧道
	return common.DB.Delete(&model.Tunnel{}, id).Error
}

// UpdateTunnelStats 更新隧道统计信息
func (s *TunnelService) UpdateTunnelStats(id uint, stats *TunnelStats) error {
	result := common.DB.Model(&model.Tunnel{}).Where("id = ?", id).Updates(map[string]interface{}{
		"connection_count":  stats.ConnectionCount,
		"total_connections": stats.TotalConnections,
		"bytes_sent":        stats.BytesSent,
		"bytes_recv":        stats.BytesRecv,
		"last_active":       stats.LastActive,
		"status":            stats.Status,
		"updated_at":        time.Now(),
	})

	return result.Error
}

// AddTunnelLog 添加隧道日志
func (s *TunnelService) AddTunnelLog(tunnelID uint, level, message, source string) error {
	log := model.TunnelLog{
		TunnelID: tunnelID,
		Level:    level,
		Message:  message,
		Source:   source,
	}

	return common.DB.Create(&log).Error
}

// GetTunnelLogs 获取隧道日志列表
func (s *TunnelService) GetTunnelLogs(tunnelID uint, limit, offset int) ([]model.TunnelLog, error) {
	var logs []model.TunnelLog
	query := common.DB.Where("tunnel_id = ?", tunnelID).Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}

	return logs, nil
}

// GetTunnelLogCount 获取隧道日志数量
func (s *TunnelService) GetTunnelLogCount(tunnelID uint) (int64, error) {
	var count int64
	if err := common.DB.Model(&model.TunnelLog{}).Where("tunnel_id = ?", tunnelID).Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

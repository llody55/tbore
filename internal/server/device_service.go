package server

import (
	"time"

	"tbore/internal/common"
	"tbore/internal/model"
)

// TagService 标签管理服务
type TagService struct{}

// NewTagService 创建标签服务实例
func NewTagService() *TagService {
	return &TagService{}
}

// CreateTag 创建标签
func (s *TagService) CreateTag(name, description string) (*model.Tag, error) {
	tag := &model.Tag{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := common.DB.Create(tag).Error; err != nil {
		return nil, err
	}

	return tag, nil
}

// GetAllTags 获取所有标签
func (s *TagService) GetAllTags() ([]model.Tag, error) {
	var tags []model.Tag
	if err := common.DB.Find(&tags).Error; err != nil {
		return nil, err
	}

	return tags, nil
}

// GetTagByID 根据ID获取标签
func (s *TagService) GetTagByID(id uint) (*model.Tag, error) {
	var tag model.Tag
	if err := common.DB.First(&tag, id).Error; err != nil {
		return nil, err
	}

	return &tag, nil
}

// UpdateTag 更新标签
func (s *TagService) UpdateTag(id uint, name, description string) (*model.Tag, error) {
	tag, err := s.GetTagByID(id)
	if err != nil {
		return nil, err
	}

	tag.Name = name
	tag.Description = description
	tag.UpdatedAt = time.Now()

	if err := common.DB.Save(tag).Error; err != nil {
		return nil, err
	}

	return tag, nil
}

// DeleteTag 删除标签
func (s *TagService) DeleteTag(id uint) error {
	// 删除标签与设备的关联
	if err := common.DB.Where("tag_id = ?", id).Delete(&model.DeviceTag{}).Error; err != nil {
		return err
	}

	// 删除标签
	return common.DB.Delete(&model.Tag{}, id).Error
}

// AddTagToDevice 给设备添加标签
func (s *TagService) AddTagToDevice(deviceID, tagID uint) error {
	// 检查设备是否存在
	var device model.Device
	if err := common.DB.First(&device, deviceID).Error; err != nil {
		return err
	}

	// 检查标签是否存在
	var tag model.Tag
	if err := common.DB.First(&tag, tagID).Error; err != nil {
		return err
	}

	// 添加关联
	association := common.DB.Model(&device).Association("Tags")
	association.Append(&tag)
	return association.Error
}

// RemoveTagFromDevice 从设备移除标签
func (s *TagService) RemoveTagFromDevice(deviceID, tagID uint) error {
	// 检查设备是否存在
	var device model.Device
	if err := common.DB.First(&device, deviceID).Error; err != nil {
		return err
	}

	// 检查标签是否存在
	var tag model.Tag
	if err := common.DB.First(&tag, tagID).Error; err != nil {
		return err
	}

	// 移除关联
	association := common.DB.Model(&device).Association("Tags")
	association.Delete(&tag)
	return association.Error
}

// GetDeviceTags 获取设备的所有标签
func (s *TagService) GetDeviceTags(deviceID uint) ([]model.Tag, error) {
	var device model.Device
	if err := common.DB.Preload("Tags").First(&device, deviceID).Error; err != nil {
		return nil, err
	}

	return device.Tags, nil
}

// DeviceService 设备管理服务
type DeviceService struct{}

// NewDeviceService 创建设备服务实例
func NewDeviceService() *DeviceService {
	return &DeviceService{}
}

// RegisterDevice 注册新设备
func (s *DeviceService) RegisterDevice(agentID, ipAddress, os, version string, tokenID uint, systemInfo model.SystemInfo, networkInfos []model.NetworkInfo, diskMounts []model.DiskMount) (*model.Device, error) {
	// 检查设备是否已存在
	var existingDevice model.Device
	if err := common.DB.Where("agent_id = ?", agentID).First(&existingDevice).Error; err == nil {
		// 设备已存在，更新状态和信息
		existingDevice.IPAddress = ipAddress
		existingDevice.OS = os
		existingDevice.Version = version
		existingDevice.TokenID = tokenID
		existingDevice.Status = "online"
		existingDevice.LastHeartbeat = time.Now()
		existingDevice.UpdatedAt = time.Now()

		// 更新设备信息
		if err := common.DB.Save(&existingDevice).Error; err != nil {
			return nil, err
		}

		// 开始事务
		tx := common.DB.Begin()

		// 检查系统信息是否已存在
		var existingSysInfo model.SystemInfo
		if err := tx.Where("device_id = ?", existingDevice.ID).First(&existingSysInfo).Error; err == nil {
			// 系统信息已存在，更新信息
			existingSysInfo.CPUModel = systemInfo.CPUModel
			existingSysInfo.CPUCores = systemInfo.CPUCores
			existingSysInfo.MemoryTotal = systemInfo.MemoryTotal
			existingSysInfo.MemoryUsed = systemInfo.MemoryUsed
			existingSysInfo.Load1 = systemInfo.Load1
			existingSysInfo.Load5 = systemInfo.Load5
			existingSysInfo.Load15 = systemInfo.Load15
			existingSysInfo.Uptime = systemInfo.Uptime
			existingSysInfo.SwapTotal = systemInfo.SwapTotal
			existingSysInfo.SwapUsed = systemInfo.SwapUsed
			existingSysInfo.UpdatedAt = time.Now()

			if err := tx.Save(&existingSysInfo).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
		} else {
			// 系统信息不存在，创建新系统信息
			systemInfo.DeviceID = existingDevice.ID
			systemInfo.CreatedAt = time.Now()
			systemInfo.UpdatedAt = time.Now()

			if err := tx.Create(&systemInfo).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
		}

		// 删除旧的网络信息
		if err := tx.Where("device_id = ?", existingDevice.ID).Delete(&model.NetworkInfo{}).Error; err != nil {
			tx.Rollback()
			return nil, err
		}

		// 保存新的网络信息
		for i := range networkInfos {
			networkInfos[i].DeviceID = existingDevice.ID
			networkInfos[i].CreatedAt = time.Now()
			networkInfos[i].UpdatedAt = time.Now()
			if err := tx.Create(&networkInfos[i]).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
		}

		// 删除旧的磁盘挂载点信息
		if err := tx.Where("device_id = ?", existingDevice.ID).Delete(&model.DiskMount{}).Error; err != nil {
			tx.Rollback()
			return nil, err
		}

		// 保存新的磁盘挂载点信息
		for i := range diskMounts {
			diskMounts[i].DeviceID = existingDevice.ID
			diskMounts[i].CreatedAt = time.Now()
			diskMounts[i].UpdatedAt = time.Now()
			if err := tx.Create(&diskMounts[i]).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
		}

		// 提交事务
		if err := tx.Commit().Error; err != nil {
			return nil, err
		}

		return &existingDevice, nil
	}

	// 创建新设备
	device := &model.Device{
		Name:          "Device-" + agentID[:8], // 默认名称
		AgentID:       agentID,
		IPAddress:     ipAddress,
		OS:            os,
		Version:       version,
		TokenID:       tokenID,
		Status:        "online",
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// 开始事务
	tx := common.DB.Begin()

	// 创建设备
	if err := tx.Create(device).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 创建系统信息
	systemInfo.DeviceID = device.ID
	systemInfo.CreatedAt = time.Now()
	systemInfo.UpdatedAt = time.Now()

	if err := tx.Create(&systemInfo).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 创建网络信息
	for i := range networkInfos {
		networkInfos[i].DeviceID = device.ID
		networkInfos[i].CreatedAt = time.Now()
		networkInfos[i].UpdatedAt = time.Now()
		if err := tx.Create(&networkInfos[i]).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// 创建磁盘挂载点信息
	for i := range diskMounts {
		diskMounts[i].DeviceID = device.ID
		diskMounts[i].CreatedAt = time.Now()
		diskMounts[i].UpdatedAt = time.Now()
		if err := tx.Create(&diskMounts[i]).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return device, nil
}

// UpdateDeviceHeartbeat 更新设备心跳
func (s *DeviceService) UpdateDeviceHeartbeat(agentID string) error {
	result := common.DB.Model(&model.Device{}).Where("agent_id = ?", agentID).Updates(map[string]interface{}{
		"status":         "online",
		"last_heartbeat": time.Now(),
		"updated_at":     time.Now(),
	})

	return result.Error
}

// GetAllDevices 获取所有设备列表
func (s *DeviceService) GetAllDevices() ([]model.Device, error) {
	var devices []model.Device
	if err := common.DB.Preload("SystemInfo").Preload("NetworkInfos").Preload("DiskMounts").Preload("Tags").Preload("Tunnels").Order("created_at DESC").Find(&devices).Error; err != nil {
		return nil, err
	}

	// 检查设备心跳，超过60秒未心跳则标记为离线
	now := time.Now()
	for i := range devices {
		if now.Sub(devices[i].LastHeartbeat) > 60*time.Second {
			// 更新设备状态为离线
			common.DB.Model(&devices[i]).Update("status", "offline")
			devices[i].Status = "offline"
		}
	}

	return devices, nil
}

// GetDeviceByAgentID 根据AgentID获取设备
func (s *DeviceService) GetDeviceByAgentID(agentID string) (*model.Device, error) {
	var device model.Device
	if err := common.DB.Preload("SystemInfo").Preload("NetworkInfos").Preload("DiskMounts").Preload("Tags").Preload("Tunnels").Where("agent_id = ?", agentID).First(&device).Error; err != nil {
		return nil, err
	}

	return &device, nil
}

// GetDeviceByID 根据ID获取设备
func (s *DeviceService) GetDeviceByID(id uint) (*model.Device, error) {
	var device model.Device
	if err := common.DB.Preload("SystemInfo").Preload("NetworkInfos").Preload("DiskMounts").Preload("Tags").Preload("Tunnels").First(&device, id).Error; err != nil {
		return nil, err
	}

	return &device, nil
}

// UpdateDevice 更新设备信息
func (s *DeviceService) UpdateDevice(id uint, name string) (*model.Device, error) {
	device, err := s.GetDeviceByID(id)
	if err != nil {
		return nil, err
	}

	device.Name = name
	device.UpdatedAt = time.Now()

	if err := common.DB.Save(device).Error; err != nil {
		return nil, err
	}

	return device, nil
}

// SetDeviceOffline 设置设备离线
func (s *DeviceService) SetDeviceOffline(agentID string) error {
	result := common.DB.Model(&model.Device{}).Where("agent_id = ?", agentID).Updates(map[string]interface{}{
		"status":     "offline",
		"updated_at": time.Now(),
	})

	return result.Error
}

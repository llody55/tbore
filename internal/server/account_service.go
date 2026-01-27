package server

import (
	"time"

	"tbore/internal/common"
	"tbore/internal/model"
)

// AccountService 账号管理服务
type AccountService struct{}

// NewAccountService 创建账号服务实例
func NewAccountService() *AccountService {
	return &AccountService{}
}

// CreateAccount 创建新账号
func (s *AccountService) CreateAccount(name, username, authType, password string, description string, isActive, isPrivileged bool) (*model.Account, error) {
	account := &model.Account{
		Name:         name,
		Username:     username,
		AuthType:     authType,
		Password:     password,
		Description:  description,
		IsActive:     isActive,
		IsPrivileged: isPrivileged,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := common.DB.Create(account).Error; err != nil {
		return nil, err
	}

	return account, nil
}

// GetAllAccounts 获取所有账号
func (s *AccountService) GetAllAccounts() ([]model.Account, error) {
	var accounts []model.Account
	if err := common.DB.Preload("Devices").Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

// GetAccountByID 根据ID获取账号
func (s *AccountService) GetAccountByID(id uint) (*model.Account, error) {
	var account model.Account
	if err := common.DB.Preload("Devices").Where("id = ?", id).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// GetAccountsByDeviceID 根据设备ID获取账号
func (s *AccountService) GetAccountsByDeviceID(deviceID uint) ([]model.Account, error) {
	var accounts []model.Account
	if err := common.DB.Where("device_id = ?", deviceID).Find(&accounts).Error; err != nil {
		return nil, err
	}
	return accounts, nil
}

// UpdateAccount 更新账号信息
func (s *AccountService) UpdateAccount(id uint, name, username, authType, password string, description string, isActive, isPrivileged bool) (*model.Account, error) {
	var account model.Account
	if err := common.DB.Where("id = ?", id).First(&account).Error; err != nil {
		return nil, err
	}

	// 更新字段
	account.Name = name
	account.Username = username
	account.AuthType = authType
	account.Password = password
	account.Description = description
	account.IsActive = isActive
	account.IsPrivileged = isPrivileged
	account.UpdatedAt = time.Now()

	if err := common.DB.Save(&account).Error; err != nil {
		return nil, err
	}

	return &account, nil
}

// BindDeviceToAccount 绑定设备到账号
func (s *AccountService) BindDeviceToAccount(accountID, deviceID uint) error {
	// 检查账号是否存在
	var account model.Account
	if err := common.DB.Where("id = ?", accountID).First(&account).Error; err != nil {
		return err
	}

	// 检查设备是否存在
	var device model.Device
	if err := common.DB.Where("id = ?", deviceID).First(&device).Error; err != nil {
		return err
	}

	// 绑定设备到账号
	assoc := common.DB.Model(&account).Association("Devices").Append(&device)
	if assoc.Error != nil {
		return assoc.Error
	}

	return nil
}

// UnbindDeviceFromAccount 从账号解绑设备
func (s *AccountService) UnbindDeviceFromAccount(accountID, deviceID uint) error {
	// 检查账号是否存在
	var account model.Account
	if err := common.DB.Where("id = ?", accountID).First(&account).Error; err != nil {
		return err
	}

	// 从账号解绑设备
	assoc := common.DB.Model(&account).Association("Devices").Delete(&model.Device{ID: deviceID})
	if assoc.Error != nil {
		return assoc.Error
	}

	return nil
}

// DeleteAccount 删除账号
func (s *AccountService) DeleteAccount(id uint) error {
	if err := common.DB.Where("id = ?", id).Delete(&model.Account{}).Error; err != nil {
		return err
	}
	return nil
}

// ToggleAccountStatus 切换账号激活状态
func (s *AccountService) ToggleAccountStatus(id uint) (*model.Account, error) {
	var account model.Account
	if err := common.DB.Where("id = ?", id).First(&account).Error; err != nil {
		return nil, err
	}

	account.IsActive = !account.IsActive
	account.UpdatedAt = time.Now()

	if err := common.DB.Save(&account).Error; err != nil {
		return nil, err
	}

	return &account, nil
}

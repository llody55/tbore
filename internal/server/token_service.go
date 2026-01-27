package server

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"

	"tbore/internal/common"
	"tbore/internal/model"
)

// TokenService Token管理服务
type TokenService struct{}

// NewTokenService 创建Token服务实例
func NewTokenService() *TokenService {
	return &TokenService{}
}

// GenerateToken 生成新的Token
func (s *TokenService) GenerateToken(name, description string) (*model.Token, error) {
	// 生成32字节的随机Token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}

	tokenValue := base64.URLEncoding.EncodeToString(tokenBytes)

	token := &model.Token{
		Name:        name,
		Value:       tokenValue,
		Description: description,
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := common.DB.Create(token).Error; err != nil {
		return nil, err
	}

	return token, nil
}

// ValidateToken 验证Token是否有效
func (s *TokenService) ValidateToken(tokenValue string) (bool, error) {
	var token model.Token
	result := common.DB.Where("value = ? AND status = ?", tokenValue, "active").First(&token)
	if result.Error != nil {
		// 检查是否是记录未找到的错误
		if result.Error.Error() == "record not found" || strings.Contains(result.Error.Error(), "record not found") {
			return false, nil
		}
		// 其他数据库错误
		return false, result.Error
	}

	return true, nil
}

// GetTokenByValue 根据Token值获取Token对象
func (s *TokenService) GetTokenByValue(tokenValue string) (*model.Token, error) {
	var token model.Token
	result := common.DB.Where("value = ?", tokenValue).First(&token)
	if result.Error != nil {
		return nil, result.Error
	}
	return &token, nil
}

// GetAllTokens 获取所有Token列表
func (s *TokenService) GetAllTokens() ([]model.Token, error) {
	var tokens []model.Token
	if err := common.DB.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, err
	}

	return tokens, nil
}

// GetTokenByID 根据ID获取Token
func (s *TokenService) GetTokenByID(id uint) (*model.Token, error) {
	var token model.Token
	if err := common.DB.First(&token, id).Error; err != nil {
		return nil, err
	}

	return &token, nil
}

// UpdateToken 更新Token信息
func (s *TokenService) UpdateToken(id uint, name, description, status string) (*model.Token, error) {
	token, err := s.GetTokenByID(id)
	if err != nil {
		return nil, err
	}

	token.Name = name
	token.Description = description
	token.Status = status
	token.UpdatedAt = time.Now()

	if err := common.DB.Save(token).Error; err != nil {
		return nil, err
	}

	return token, nil
}

// DeleteToken 删除Token
func (s *TokenService) DeleteToken(id uint) error {
	return common.DB.Delete(&model.Token{}, id).Error
}

package common

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/viper"
)

// Config 全局配置结构体
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int
	Host string
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Path string
}

var AppConfig Config

// LoadConfig 加载配置文件
func LoadConfig(configPath string) error {
	if configPath == "" {
		configPath = "./config.yaml"
	}

	// 设置默认值，防止配置文件不存在或解析失败
	viper.SetDefault("server.port", 7835)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("database.path", "./tbore.db")

	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 创建默认配置文件
		defaultConfig := `server:
  port: 7835
  host: "0.0.0.0"
database:
  path: "./tbore.db"`
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
			return fmt.Errorf("failed to create default config file: %v", err)
		}
		log.Printf("Created default config file: %s", configPath)
	} else {
		// 配置文件存在，读取配置
		viper.SetConfigFile(configPath)
		viper.SetConfigType("yaml")

		if err := viper.ReadInConfig(); err != nil {
			log.Printf("Warning: failed to read config file, using default values: %v", err)
		}
	}

	// 绑定配置到结构体
	if err := viper.Unmarshal(&AppConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %v", err)
	}

	log.Printf("Config loaded from %s", configPath)
	log.Printf("Database path: %s", AppConfig.Database.Path)
	return nil
}

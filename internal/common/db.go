package common

import (
	"log"

	"tbore/internal/model"

	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(dbPath string) error {
	var err error

	// 连接SQLite数据库
	DB, err = gorm.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}

	// 启用日志
	DB.LogMode(true)

	// 自动迁移数据库表
	err = DB.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.Device{},
		&model.Account{},
		&model.AccountDevice{},
		&model.SystemInfo{},
		&model.NetworkInfo{},
		&model.DiskMount{},
		&model.Tag{},
		&model.DeviceTag{},
		&model.Tunnel{},
	).Error
	if err != nil {
		return err
	}

	// 初始化默认管理员用户
	initDefaultUser()

	log.Printf("Database initialized successfully: %s", dbPath)
	return nil
}

// 初始化默认管理员用户
func initDefaultUser() {
	var count int
	DB.Model(&model.User{}).Count(&count)
	if count == 0 {
		admin := model.User{
			Username: "admin",
			Password: "admin123", // 实际项目中应该使用加密密码
			Role:     "admin",
		}
		DB.Create(&admin)
		log.Println("Created default admin user: admin/admin123")
	}
}

// CloseDatabase 关闭数据库连接
func CloseDatabase() {
	if DB != nil {
		DB.Close()
	}
}

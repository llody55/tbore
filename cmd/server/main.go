package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"tbore/internal/common"
	"tbore/internal/server"
)

var (
	configPath string
	version    = "0.1.0"
)

func init() {
	// 命令行参数解析
	flag.StringVar(&configPath, "c", "./config.yaml", "Path to config file")
	flag.Parse()
}

func main() {
	log.Printf("Starting tbore-pro server v%s...", version)

	// 1. 加载配置文件
	if err := common.LoadConfig(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化数据库
	if err := common.InitDatabase(common.AppConfig.Database.Path); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer common.CloseDatabase()

	// 3. 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 4. 创建Gin路由器
	router := gin.Default()

	// 5. 注册API路由
	handler := server.NewHandler()
	handler.RegisterRoutes(router)

	// 6. 静态文件服务（用于Web管理界面）
	router.Static("/web", "./web")
	router.GET("/", func(c *gin.Context) {
		c.Redirect(302, "/web/")
	})

	// 7. 启动服务器
	addr := fmt.Sprintf("%s:%d", common.AppConfig.Server.Host, common.AppConfig.Server.Port)
	log.Printf("Server started on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"tbore/internal/common"
	"tbore/internal/server"
)

func main() {
	// 加载配置
	if err := common.LoadConfig(""); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化数据库
	if err := common.InitDatabase(common.AppConfig.Database.Path); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer common.CloseDatabase()

	// 初始化Gin引擎
	router := gin.Default()

	// 创建并注册API处理器
	handler := server.NewHandler()
	handler.RegisterRoutes(router)

	// 静态文件服务
	router.Static("/web", "./web")
	router.GET("/", func(c *gin.Context) {
		c.Redirect(302, "/web/")
	})

	// 启动服务器
	addr := common.AppConfig.Server.Host
	port := common.AppConfig.Server.Port
	log.Printf("Server starting on %s:%d", addr, port)
	if err := router.Run("0.0.0.0:7835"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
# TBORE Pro - 企业级设备管理系统

## 项目概述

TBORE Pro是一个企业级设备管理系统，专为管理和监控网络设备而设计。它提供了设备管理、隧道管理、WebSSH连接等功能，帮助企业实现对网络设备的集中管理和远程访问。

### 核心功能

- **设备管理**：添加、编辑、删除设备，监控设备状态
- **账户管理**：创建、编辑、禁用/启用账户，支持账户与设备绑定
- **隧道管理**：创建和管理设备隧道，支持端口映射
- **WebSSH连接**：通过浏览器访问设备终端，支持账户选择
- **数据统计**：设备状态统计，连接历史记录

### 技术栈

- **后端**：Go 1.24.11 + Gin框架
- **数据库**：SQLite + GORM ORM
- **前端**：LayUI + jQuery
- **通信**：WebSocket（WebSSH）

## 安装说明

### 前置条件

- Go 1.24.11或更高版本
- SQLite 3
- 现代Web浏览器

### 安装步骤

1. **克隆项目**

```bash
git clone https://github.com/yourusername/tbore-pro.git
cd tbore-pro
```

2. **安装依赖**

```bash
go mod download
```

3. **构建项目**

```bash
go build -o tbore-server main.go
```

4. **启动服务**

```bash
./tbore-server
```

服务默认运行在 `http://localhost:7835`

## 快速开始

1. **访问系统**

打开浏览器，访问 `http://localhost:7835`

2. **登录系统**

默认管理员账户：
- 用户名：admin
- 密码：admin123

3. **添加设备**

在设备管理页面，点击"添加设备"按钮，填写设备信息并提交。

4. **创建账户**

在账户管理页面，点击"添加账户"按钮，填写账户信息并提交。

5. **绑定账户到设备**

在设备管理页面，选择设备，点击"更多"下拉菜单，选择"绑定账户"，选择要绑定的账户并提交。

6. **WebSSH连接**

在设备管理页面，选择设备，点击"Web终端"按钮，选择绑定的账户或手动输入账户信息，点击"连接"按钮。

## 目录结构

```
tbore-pro/
├── internal/            # 内部包
│   ├── common/          # 公共组件
│   ├── model/           # 数据模型
│   ├── server/          # 服务器组件
│   └── service/         # 业务逻辑
├── web/                 # 前端资源
│   ├── js/              # JavaScript文件
│   ├── layui/           # LayUI库
│   ├── pages/           # 页面文件
│   └── index.html       # 首页
├── config.yaml          # 配置文件
├── main.go              # 入口文件
├── go.mod               # Go模块文件
└── README.md            # 项目文档
```

## 配置说明

配置文件位于 `config.yaml`，支持以下配置项：

- `server.port`：服务器端口
- `server.host`：服务器主机
- `database.path`：数据库路径
- `websocket.timeout`：WebSocket超时时间

## 许可证

MIT License

## 贡献

欢迎贡献代码和提出问题！请提交Pull Request或Issue。

## 联系方式

- 项目地址：https://github.com/yourusername/tbore-pro
- 邮箱：your.email@example.com
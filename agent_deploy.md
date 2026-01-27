# Agent部署指南

## 1. 环境准备

### 1.1 安装Go环境

在部署Agent之前，需要确保目标主机已安装Go 1.16或更高版本。

```bash
go version
```

如果未安装Go，可以参考[官方安装指南](https://go.dev/doc/install)。

### 1.2 下载代码

将tbore-pro代码复制到目标主机：

```bash
# 使用scp命令复制代码
scp -r /root/go-build/tbore-pro user@target-host:/path/to/tbore-pro
```

或者使用git克隆（如果有远程仓库）：

```bash
git clone https://github.com/your-repo/tbore-pro.git
```

## 2. 配置Agent

### 2.1 配置文件

Agent支持通过配置文件进行配置。在Agent目录下创建`config.json`文件：

```json
{
  "ServerAddr": "http://your-server-ip:7835",
  "Token": "your-token",
  "AgentID": "",
  "Version": "1.0.0"
}
```

**配置说明：**

- `ServerAddr`: 服务器地址，格式为`http://ip:port`
- `Token`: 用于验证Agent的令牌，需要在服务器端创建
- `AgentID`: 可选，如果不提供，Agent会自动生成
- `Version`: Agent版本，用于标识Agent版本

### 2.2 命令行参数

Agent还支持通过命令行参数覆盖配置文件：

```bash
go run agent.go --server http://your-server-ip:7835 --token your-token
```

## 3. 运行Agent

### 3.1 直接运行

在Agent目录下执行：

```bash
go run agent.go
```

### 3.2 编译后运行

如果需要在没有Go环境的主机上运行，可以先编译：

```bash
# 编译当前平台版本
go build -o tbore-agent agent.go

# 编译跨平台版本
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tbore-agent-linux-amd64 agent.go
```

然后在目标主机上运行编译后的二进制文件：

```bash
chmod +x tbore-agent
./tbore-agent
```

### 3.3 作为系统服务运行

在Linux系统上，可以将Agent配置为系统服务，实现开机自启：

创建服务文件 `/etc/systemd/system/tbore-agent.service`：

```ini
[Unit]
Description=tbore-pro Agent
After=network.target

[Service]
Type=simple
ExecStart=/path/to/tbore-agent
Restart=always
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
```

启动服务并设置开机自启：

```bash
systemctl daemon-reload
systemctl start tbore-agent
systemctl enable tbore-agent
```

查看服务状态：

```bash
systemctl status tbore-agent
```

## 4. 测试Web终端功能

### 4.1 在服务器端创建Token

在Web界面中创建一个Token，用于Agent注册：

1. 访问服务器Web界面：`http://your-server-ip:7835`
2. 进入"系统管理" -> "Token管理"
3. 点击"新增"按钮创建Token
4. 记录生成的Token值

### 4.2 运行Agent

使用生成的Token在目标主机上运行Agent：

```bash
go run agent.go --server http://your-server-ip:7835 --token your-token
```

### 4.3 在Web界面查看设备

1. 访问服务器Web界面：`http://your-server-ip:7835`
2. 进入"系统管理" -> "设备管理"
3. 查看新注册的设备是否显示在列表中
4. 确保设备状态为"在线"

### 4.4 测试Web终端

1. 在设备管理页面，找到刚刚注册的设备
2. 点击设备操作栏中的"Web终端"按钮
3. 在弹出的登录窗口中，输入目标主机的SSH用户名、密码和端口
4. 点击"连接"按钮
5. 等待连接建立，即可在浏览器中使用终端

## 5. 常见问题

### 5.1 Agent无法连接到服务器

- 检查服务器地址和端口是否正确
- 检查防火墙是否允许Agent连接到服务器
- 检查Token是否正确

### 5.2 Web终端无法连接到设备

- 检查设备是否在线
- 检查设备的SSH服务是否运行
- 检查设备的SSH端口是否正确
- 检查用户名和密码是否正确

### 5.3 Agent频繁断开连接

- 检查网络稳定性
- 检查服务器和Agent之间的网络延迟
- 检查服务器负载是否过高

## 6. 日志查看

Agent的日志会输出到控制台，可以通过以下方式查看：

### 6.1 直接运行时查看

```bash
go run agent.go > agent.log 2>&1 &
tail -f agent.log
```

### 6.2 系统服务日志

```bash
journalctl -u tbore-agent -f
```

## 7. 配置示例

### 7.1 基本配置示例

```json
{
  "ServerAddr": "http://192.168.1.100:7835",
  "Token": "abcdef1234567890",
  "Version": "1.0.0"
}
```

### 7.2 完整配置示例

```json
{
  "ServerAddr": "http://192.168.1.100:7835",
  "Token": "abcdef1234567890",
  "AgentID": "agent-123456",
  "Version": "1.0.0"
}
```

## 8. 技术支持

如果在部署或使用过程中遇到问题，可以查看Agent日志或服务器日志，或联系技术支持。

### 8.1 Agent日志位置

- 直接运行：控制台输出
- 系统服务：`journalctl -u tbore-agent`

### 8.2 服务器日志位置

服务器日志默认输出到控制台，可以通过以下方式查看：

```bash
# 查看服务器运行日志
tail -f server.log
```

---

通过以上步骤，您可以在其他主机上成功部署Agent并测试Web终端功能。祝您使用愉快！
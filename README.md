## tbore (Tunnel over SSH)

[![Go](https://img.shields.io/badge/Go-1.21%2B-blue)](https://golang.org)

`tbore` 是一个基于 **SSH 协议** 实现的高性能内网穿透与端口转发工具。它利用 SSH 原生的多路复用（Multiplexing）技术，能够在一个物理连接中安全、稳定地转发高并发的 TCP 流量，完美支持 MySQL、Redis、SSH 及 HTTP 等复杂协议。

### 🚀 核心特性

- **多路复用 (Multiplexing)** ：原生支持并发连接，彻底解决数据库连接池等高并发场景下的链路阻塞问题。
- **工业级安全** ：底层基于标准 SSH 协议，全量流量 AES 加密，支持 HMAC-SHA256 挑战-响应认证。
- **零配置启动** ：服务端自动生成并持久化存储 RSA 密钥，无需手动配置证书或密钥文件。
- **无依赖单文件** ：支持纯静态编译，可在任何 Linux 发行版运行。
- **自动资源回收** ：服务端会话绑定技术，客户端下线后，其占用的公网端口将 **立即释放**。
- **心跳检测 (Keepalive)** ：内置双向心跳，有效应对运营商长连接清理及复杂的防火墙环境。
- **自动重连** ：客户端具备心跳检测与断线自动重连机制，确保隧道长期可用。
- **配置热加载** ：支持动态增删改隧道配置，**不中断主 SSH 连接**。

### 📦 安装与编译

```bash
git clone https://github.com/llody55/tbore.git
cd tbore
go mod tidy

# 静态编译
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o tbore tbore.go
```

### 🛠 快速上手

#### 1. 服务端配置 (Server)

创建 `server.yaml` 配置文件：

```yaml
port: 7835
token: "your_secure_secret_token"
min_port: 1024
max_port: 65535
max_connections: 100
max_tunnels_per_client: 10
bind_addr: "0.0.0.0"
host_key_path: "./host_key"
```

启动服务端：

```bash
./tbore server -c server.yaml
```

服务端启动后会显示主机密钥指纹：

```
tbore server v0.4.0 started on 0.0.0.0:7835
Host key fingerprint: SHA256:xxxx...
```

#### 2. 客户端配置 (Client)

创建 `client.yaml` 配置文件：

```yaml
server_addr: "你的服务器IP"
server_port: 7835
token: "your_secure_secret_token"
host_key: "SHA256:xxxx..."
project: "your_project"
region: "your_region"
health_check_interval: 30
tunnels:
  - name: "web-service"
    local_ip: "127.0.0.1"
    local_port: 8080
    remote_port: 8501

  - name: "ssh-tunnel"
    local_ip: "192.168.1.10"
    local_port: 22
    remote_port: 0
```

#### 3. 启动客户端

```bash
./tbore client -c client.yaml
```

### ⚙️ 配置说明

**服务端配置 (server.yaml)**：

| 配置项                       | 类型     | 默认值     | 说明         |
| ------------------------- | ------ | ------- | ---------- |
| port                      | int    | 7835    | 控制端口       |
| token                     | string | -       | 认证密钥（必填）   |
| min\_port                 | uint32 | 1024    | 最小允许绑定端口   |
| max\_port                 | uint32 | 65535   | 最大允许绑定端口   |
| max\_connections          | int    | 100     | 最大并发连接数    |
| max\_tunnels\_per\_client | int    | 10      | 每个客户端最大隧道数 |
| bind\_addr                | string | 0.0.0.0 | 绑定地址       |
| host\_key\_path           | string | -       | 主机密钥存储路径   |

**客户端配置 (client.yaml)**：

| 配置项                     | 类型     | 说明                  |
| ----------------------- | ------ | ------------------- |
| server\_addr            | string | 服务端地址               |
| server\_port            | int    | 服务端端口               |
| token                   | string | 认证密钥                |
| host\_key               | string | 服务端主机密钥指纹（可选）       |
| project                 | string | 项目名称（用于服务端识别和管理）    |
| region                  | string | 地域/数据中心（用于服务端识别和管理） |
| tunnels                 | array  | 隧道配置列表              |
| health\_check\_interval | int    | 健康检查间隔（秒）,默认30      |

### 🏗️ 技术架构

1. **握手阶段** ：客户端生成随机挑战，计算 HMAC 响应，通过 SSH 连接服务端完成认证。
2. **端口请求** ：客户端发起 `tcpip-forward` 请求，服务端验证端口范围后动态开启监听。
3. **多路复用** ：当公网用户连接该端口时，服务端开启 `forwarded-tcpip` 逻辑通道。
4. **双向转发** ：数据通过 SSH 隧道流向客户端，客户端与本地服务建立连接并进行转发。

### 📊 安全特性

| 特性             | 说明               |
| -------------- | ---------------- |
| HMAC-SHA256 认证 | 防暴力破解、防重放攻击      |
| 端口范围限制         | 防止绑定特权端口（1-1023） |
| 连接数限制          | 防止 DoS 攻击        |
| 隧道数限制          | 防止端口耗尽           |
| 主机密钥验证         | 防止中间人攻击          |

### 📅 版本更新记录

#### v0.6.2

- **\[CRITICAL FIX]** 修复高并发场景下内存泄漏问题，引入 `sync.Pool` 复用连接缓冲区。
- **\[CRITICAL FIX]** 修复服务端 `handleClientConnection` 函数中活跃连接计数递减不完整的问题。
- **\[PERF]** 使用缓冲区池替代每次连接分配新缓冲区，解决高并发场景下内存占用过高的问题。
- **\[PERF]** 服务端和客户端均应用缓冲区池优化，全面提升高并发场景下的内存效率。

#### v0.6.1

- **\[CRITICAL FIX]** 修复文件描述符泄漏问题，确保连接关闭后资源及时释放。
- **\[FIX]** 修复 SSH 握手失败时底层 TCP 连接未关闭的问题。
- **\[FIX]** 修复 SSH 通道打开失败时通道资源未释放的问题。
- **\[FIX]** 修复双向数据拷贝时一个方向关闭导致另一个方向永久阻塞的问题。
- **\[IMPROVE]** Uptime 显示格式优化，支持天、时、分、秒完整显示（如 `5d 7h 30m 45s`）。

#### v0.6.0

- **\[NEW]** 新增配置热加载功能，支持动态增删改隧道配置。
- **\[NEW]** 新增 `tbore reload` 命令，一键触发配置重载。
- **\[NEW]** 新增 `tbore client-status` 命令，查看客户端隧道状态。
- **\[NEW]** 客户端支持 Unix socket 控制接口（默认 `/var/run/tbore-client.sock`）。
- **\[NEW]** 支持 SIGHUP 信号触发热加载，便于运维自动化。
- **\[IMPROVE]** 隧道智能增量更新，只处理变化的隧道，保持主连接不中断。
- **\[IMPROVE]** 删除隧道时自动通知服务端释放端口，避免端口占用。

#### v0.5.4

- **\[NEW]** 新增隧道健康检查机制，客户端定期探测后端服务可用性。
- **\[NEW]** 新增 `health_check_interval` 配置项（默认30秒），支持自定义健康检查间隔。
- **\[NEW]** 隧道状态显示优化，支持显示 UP/IDLE/DOWN 三种状态。
- **\[FIX]** 修复连接计数泄漏问题，使用 defer 确保计数正确递减。
- **\[FIX]** 修复状态更新不及时问题，后端服务不可用时立即更新状态。
- **\[IMPROVE]** 优化日志输出，健康状态仅在变化时记录，减少日志噪声。
- **\[IMPROVE]** 状态显示优化，DOWN 状态不再显示活跃连接数，避免误解。

#### v0.5.3

- **\[CRITICAL FIX]** 修复通道处理启动时机过晚导致的 "administratively prohibited" 错误。
- **\[FIX]** 优化通道处理逻辑，确保客户端在服务端发送通道请求前已准备就绪。

#### v0.5.2

- **\[CRITICAL FIX]** 修复隧道映射完全不可用的问题（客户端未处理服务端发起的通道请求）。
- **\[FIX]** 修复地址解析错误，`msg.Addr` 已经是完整地址格式，不需要再次拼接端口。

#### v0.5.1

- **\[NEW]** 隧道信息显示优化，支持显示隧道名称和完整的本地地址映射。
- **\[FIX]** 修复认证数据长度不足导致的 panic 问题。
- **\[FIX]** 修复错误日志显示信息不足的问题。
- **\[FIX]** 修复客户端配置缺少验证的问题。
- **\[FIX]** 修复资源泄漏问题（local 连接未正确关闭）。
- **\[FIX]** 修复 copyBuffer 函数写入错误被忽略的问题。

#### v0.5.0

- **\[NEW]** 新增 `tbore status` 命令，支持查看服务端实时连接状态和隧道映射信息。
- **\[NEW]** 客户端支持配置 `project` 和 `region` 字段，便于服务端识别和管理。
- **\[NEW]** 服务端状态输出支持表格化展示，包含连接概览和隧道详情。
- **\[FIX]** 修复客户端断开后端口未正确释放的问题。
- **\[REFACTOR]** 版本号统一管理，抽取独立的 `pkg/version` 包。

#### v0.4.0

- **\[SECURITY]** 引入 HMAC-SHA256 挑战-响应认证机制，防止暴力破解和重放攻击。
- **\[SECURITY]** 实现端口范围限制（默认 1024-65535），防止绑定特权端口。
- **\[SECURITY]** 实现连接数限制（默认 100）和隧道数限制（默认 10）。
- **\[SECURITY]** 强制要求 Token 认证，服务端必须配置 Token 才能启动。
- **\[NEW]** 服务端支持配置文件启动。
- **\[NEW]** 主机密钥持久化存储，支持客户端验证。
- **\[REFACTOR]** 项目重构为模块化结构。

#### v0.3.1

- **\[性能]** 引入 `TCP_NODELAY` 算法优化。
- **\[吞吐]** 将内部转发缓冲区提升至 128KB。

#### v0.3.0

- **\[BREAKING]** 升级 SSH 转发载荷至 4 字段结构。
- **\[NEW]** 服务端引入 `sync.Map` 管理监听器。

### 📜 开源协议

MIT License

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

| 配置项          | 类型     | 说明                  |
| ------------ | ------ | ------------------- |
| server\_addr | string | 服务端地址               |
| server\_port | int    | 服务端端口               |
| token        | string | 认证密钥                |
| host\_key    | string | 服务端主机密钥指纹（可选）       |
| project      | string | 项目名称（用于服务端识别和管理）    |
| region       | string | 地域/数据中心（用于服务端识别和管理） |
| tunnels      | array  | 隧道配置列表              |

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

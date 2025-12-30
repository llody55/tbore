## tbore (Tunnel over SSH)

[![Go](https://img.shields.io/badge/Go-1.21%2B-blue)](https://golang.org)

`tbore` 是一个基于 **SSH 协议** 实现的高性能内网穿透与端口转发工具。它利用 SSH 原生的多路复用（Multiplexing）技术，能够在一个物理连接中安全、稳定地转发高并发的 TCP 流量，完美支持 MySQL、Redis、SSH 及 HTTP 等复杂协议。

### 🚀 核心特性

* **多路复用 (Multiplexing)** ：原生支持并发连接，彻底解决数据库连接池等高并发场景下的链路阻塞问题。
* **工业级安全** ：底层基于标准 SSH 协议，全量流量 AES 加密，支持 Token 身份验证。
* **零配置启动** ：服务端自动在内存中生成 2048 位 RSA 密钥，无需手动配置证书或密钥文件。
* **无依赖单文件** ：支持纯静态编译，可在任何 Linux 发行版（CentOS, Ubuntu, Alpine 等）运行，无 `glibc` 版本依赖。
* **自动资源回收** ：服务端会话绑定技术，客户端下线后，其占用的公网端口将 **立即释放** ，彻底解决 `address already in use` 报错。
* **心跳检测 (Keepalive)** ：内置双向心跳，有效应对运营商长连接清理及复杂的防火墙环境。
* **自动重连** ：客户端具备心跳检测与断线自动重连机制，确保隧道长期可用。

---

### 📦 安装与编译

为了确保在不同版本的 Linux（如旧版 CentOS7）上都能正常运行，推荐使用 Go 1.21+ 版本进行编译，为了确保最佳兼容性，建议使用以下命令进行 **静态编译** ：

```bash
# 克隆仓库并安装依赖
go mod init tbore
go get golang.org/x/crypto/ssh
go get gopkg.in/yaml.v3
go mod tidy

# 静态编译（跨系统运行的终极方案）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o tbore tbore.go
```

---

### 🛠 快速上手

#### 1. 服务端 (部署在公网服务器)

启动服务端并监听 `7835` 端口（默认），同时设置验证 Token。

```bash
./tbore server --port 7835 --token your_secret_token
```

#### 2. 客户端配置 (Client)

创建 `config.yaml` 配置文件：

```yaml
client:
  server_addr: "你的服务器IP"
  server_port: 7835
  token: "your_secure_token"
  tunnels:
    - name: "web-service"
      local_ip: "127.0.0.1"
      local_port: 8080
      remote_port: 8501   # 固定端口转发

    - name: "ssh-tunnel"
      local_ip: "192.168.1.10"
      local_port: 22
      remote_port: 0      # 随机端口转发
```

#### 3. 客户端 (部署在内网机器)

```bash
./tbore client -c config.yaml
```

### 📖 进阶用法：Systemd 后台运行

为了保证服务在 Linux 后台稳定运行，建议使用 Systemd 管理。

**服务端配置 (`/etc/systemd/system/tbore-server.service`):**

```ini
[Unit]
Description=tbore Reverse Proxy Server
After=network.target

[Service]
Type=simple
ExecStart=/path/to/tbore server --port 7835 --token your_token
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### 🏗️ 技术架构

`tbore` 的工作流程如下：

1. **握手阶段** ：客户端通过 TCP 连接服务端，并升级为 SSH 连接，完成 Token 验证。
2. **端口请求** ：客户端发起 `tcpip-forward` 全局请求，服务端动态开启一个公网监听端口。
3. **多路复用** ：当公网用户连接该端口时，服务端开启一个 `forwarded-tcpip` 逻辑通道。
4. **双向转发** ：数据通过 SSH 隧道流向客户端，客户端与本地服务建立连接并进行 `io.Copy`。

---

### 📊 性能与使用边界 (Performance Guidelines)

由于 `tbore` 目前基于 SSH (TCP) 隧道实现，其表现受限于物理网络延迟（RTT）。

| **网络延迟 (RTT)** | **建议用途**    | **远程桌面 (RDP) 体验**      |
| ------------------------ | --------------------- | ---------------------------------- |
| **< 20ms**         | 任何服务              | **极度顺滑**(同城/内网级)    |
| **20ms - 50ms**    | 数据库、Web、远程桌面 | **良好**(跨省，略有拖拽感)   |
| **50ms - 100ms**   | 文件传输、SSH 命令行  | **一般**(会有明显的画面滞后) |
| **> 100ms**        | 基础监控、轻量指令    | **不建议用于远程桌面**       |

### ⚠️ 注意事项

* **防火墙** ：请确保公网服务器的 `7835` 端口以及 tbore 动态分配的随机端口（如上面的 `8501`）在安全组/防火墙中已开放。
* **自动重连** ：客户端内置了断线重连机制，当网络波动导致连接断开时，它每隔 5 秒会自动尝试恢复隧道。

### 📅 版本更新记录

v0.3.0 引入了协议层面的重大变更，与 v0.2.x 版本不兼容。

#### v0.3.1(Latest)

* **[性能]** 引入 `TCP_NODELAY` 算法优化，大幅降低了 RDP 远程桌面在 50ms 延迟内的操作迟滞。
* **[吞吐]** 将内部转发缓冲区提升至 128KB。
* **[修复]** 进一步加固了 `sync.Map` 在极端网络波动下的资源回收机制。

#### v0.3.0

* **[BREAKING]** 升级 SSH 转发载荷至 4 字段结构，提升协议标准一致性。
* **[NEW]** 服务端引入 `sync.Map` 管理监听器，实现端口与连接生命周期的强绑定。
* **[FIX]** 修复了在高并发连接下，旧监听器未关闭导致新连接无法绑定的 Bug。
* **[FIX]** 修复了在某些 Linux 发行版上出现的 `port number out of range: 0` 错误。

#### v0.2.3

* 实现了基础的 Token 验证。
* 支持 YAML 配置文件驱动多个隧道。
* 加入了基础的 Keepalive 机制。

### 📜 开源协议

MIT License

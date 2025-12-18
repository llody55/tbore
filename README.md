
## tbore (Tunnel over SSH)

[![Go](https://img.shields.io/badge/Go-1.21%2B-blue)](https://golang.org)

`tbore` 是一个基于 **SSH 协议** 实现的高性能内网穿透与端口转发工具。它利用 SSH 原生的多路复用（Multiplexing）技术，能够在一个物理连接中安全、稳定地转发高并发的 TCP 流量，完美支持 MySQL、Redis、SSH 及 HTTP 等复杂协议。

### 🚀 核心特性

* **多路复用 (Multiplexing)** ：原生支持并发连接，彻底解决数据库连接池等高并发场景下的链路阻塞问题。
* **工业级安全** ：底层基于标准 SSH 协议，全量流量 AES 加密，支持 Token 身份验证。
* **零配置启动** ：服务端自动在内存中生成 2048 位 RSA 密钥，无需手动配置证书或密钥文件。
* **无依赖单文件** ：支持纯静态编译，可在任何 Linux 发行版（CentOS, Ubuntu, Alpine 等）运行，无 `glibc` 版本依赖。
* **自动重连** ：客户端具备心跳检测与断线自动重连机制，确保隧道长期可用。

---

### 📦 安装与编译

为了确保在不同版本的 Linux（如旧版 CentOS7）上都能正常运行，推荐使用 Go 1.21+ 版本进行编译，为了确保最佳兼容性，建议使用以下命令进行 **静态编译** ：

```bash
# 克隆仓库并安装依赖
go mod init tbore
go get golang.org/x/crypto/ssh
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

#### 2. 客户端 (部署在内网机器)

将内网的 MySQL (3306) 转发到公网服务器。

```bash
./tbore client --to <服务器公网IP> --port 7835 --local-port 3306 --token your_secret_token
```

#### 3. 访问服务

客户端启动成功后会输出：

Public Access -> <服务器公网IP>:<随机端口>

你只需要使用 MySQL 客户端连接该服务器的随机端口即可。

### 📖 命令行参数说明

| **参数**   | **描述**                          | **默认值** |
| ---------------- | --------------------------------------- | ---------------- |
| `--port`       | 服务端监听端口 / 客户端连接服务端的端口 | 7835             |
| `--token`      | 身份验证令牌                            | 空 (不验证)      |
| `--to`         | (仅客户端) 服务端公网 IP 地址           | 无               |
| `--local-port` | (仅客户端) 需要暴露的本地服务端口       | 8080             |

### 💡 典型应用场景：转发 MySQL

1. **启动 Server** ：服务器显示 `tbore server v0.2.3-ssh listening on :7835`。
2. **启动 Client** ：客户端连接成功后显示 `Public Access -> 服务器IP:45678`。
3. **远程连接** ：
   你现在可以使用任何数据库工具通过公网 IP 和端口 `45678` 访问内网的 MySQL 了：

```bash
   mysql -h <服务器IP> -P 45678 -u root -p
```

### 🏗️ 技术架构

`tbore` 的工作流程如下：

1. **握手阶段** ：客户端通过 TCP 连接服务端，并升级为 SSH 连接，完成 Token 验证。
2. **端口请求** ：客户端发起 `tcpip-forward` 全局请求，服务端动态开启一个公网监听端口。
3. **多路复用** ：当公网用户连接该端口时，服务端开启一个 `forwarded-tcpip` 逻辑通道。
4. **双向转发** ：数据通过 SSH 隧道流向客户端，客户端与本地服务建立连接并进行 `io.Copy`。

---

### ⚠️ 注意事项

* **防火墙** ：请确保公网服务器的 `7835` 端口以及 tbore 动态分配的随机端口（如上面的 `45678`）在安全组/防火墙中已开放。
* **自动重连** ：客户端内置了断线重连机制，当网络波动导致连接断开时，它每隔 5 秒会自动尝试恢复隧道。

---

### 📜 开源协议

MIT License

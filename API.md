# TBORE Pro API文档

## 概述

本文档描述了TBORE Pro系统的API端点、请求参数和响应格式。API使用RESTful设计风格，支持JSON格式的请求和响应。

## 基础URL

所有API端点的基础URL为：

```
http://localhost:7835/api/v1
```

## 认证

系统使用Token认证机制，需要在请求头中包含`Authorization`字段：

```
Authorization: Bearer <token>
```

## API端点

### 1. 账户管理

#### 1.1 创建账户

- **URL**: `/accounts`
- **方法**: `POST`
- **请求体**:

```json
{
  "name": "Linux通用",
  "username": "root",
  "password": "password123",
  "auth_type": "password",
  "description": "管理员账户",
  "is_active": true,
  "is_privileged": true
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "Linux通用",
    "username": "root",
    "auth_type": "password",
    "description": "管理员账户",
    "is_active": true,
    "is_privileged": true,
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T10:00:00Z"
  }
}
```

#### 1.2 获取所有账户

- **URL**: `/accounts`
- **方法**: `GET`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": [
    {
      "id": 1,
      "name": "Linux通用",
      "username": "root",
      "auth_type": "password",
      "description": "管理员账户",
      "is_active": true,
      "is_privileged": true,
      "created_at": "2026-01-27T10:00:00Z",
      "updated_at": "2026-01-27T10:00:00Z"
    }
  ]
}
```

#### 1.3 获取单个账户

- **URL**: `/accounts/{id}`
- **方法**: `GET`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "Linux通用",
    "username": "root",
    "auth_type": "password",
    "description": "管理员账户",
    "is_active": true,
    "is_privileged": true,
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T10:00:00Z"
  }
}
```

#### 1.4 更新账户

- **URL**: `/accounts/{id}`
- **方法**: `PUT`
- **请求体**:

```json
{
  "name": "Linux通用(更新)",
  "username": "admin",
  "password": "newpassword123",
  "auth_type": "password",
  "description": "更新后的管理员账户",
  "is_active": true,
  "is_privileged": true
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "name": "Linux通用(更新)",
    "username": "admin",
    "auth_type": "password",
    "description": "更新后的管理员账户",
    "is_active": true,
    "is_privileged": true,
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T11:00:00Z"
  }
}
```

#### 1.5 删除账户

- **URL**: `/accounts/{id}`
- **方法**: `DELETE`
- **响应**:

```json
{
  "code": 200,
  "msg": "success"
}
```

#### 1.6 切换账户状态

- **URL**: `/accounts/{id}/status`
- **方法**: `PUT`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "is_active": false
  }
}
```

#### 1.7 绑定设备到账户

- **URL**: `/accounts/{id}/devices`
- **方法**: `POST`
- **请求体**:

```json
{
  "device_id": 1
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "Device bound to account successfully"
}
```

#### 1.8 从账户解绑设备

- **URL**: `/accounts/{id}/devices/{device_id}`
- **方法**: `DELETE`
- **响应**:

```json
{
  "code": 200,
  "msg": "Device unbound from account successfully"
}
```

### 2. 设备管理

#### 2.1 注册设备

- **URL**: `/devices/register`
- **方法**: `POST`
- **请求体**:

```json
{
  "agent_id": "test-agent-123",
  "name": "Device-test-age",
  "ip_address": "192.168.1.100",
  "os": "Linux"
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "agent_id": "test-agent-123",
    "name": "Device-test-age",
    "ip_address": "192.168.1.100",
    "os": "Linux",
    "status": "online",
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T10:00:00Z"
  }
}
```

#### 2.2 更新设备心跳

- **URL**: `/devices/heartbeat`
- **方法**: `POST`
- **请求体**:

```json
{
  "agent_id": "test-agent-123",
  "ip_address": "192.168.1.100"
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success"
}
```

#### 2.3 获取所有设备

- **URL**: `/devices`
- **方法**: `GET`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": [
    {
      "id": 1,
      "agent_id": "test-agent-123",
      "name": "Device-test-age",
      "ip_address": "192.168.1.100",
      "os": "Linux",
      "status": "online",
      "last_heartbeat": "2026-01-27T10:00:00Z",
      "created_at": "2026-01-27T10:00:00Z",
      "updated_at": "2026-01-27T10:00:00Z"
    }
  ]
}
```

#### 2.4 获取单个设备

- **URL**: `/devices/{id}`
- **方法**: `GET`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "agent_id": "test-agent-123",
    "name": "Device-test-age",
    "ip_address": "192.168.1.100",
    "os": "Linux",
    "status": "online",
    "last_heartbeat": "2026-01-27T10:00:00Z",
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T10:00:00Z"
  }
}
```

#### 2.5 更新设备

- **URL**: `/devices/{id}`
- **方法**: `PUT`
- **请求体**:

```json
{
  "name": "Device-test-age(更新)",
  "status": "online"
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "agent_id": "test-agent-123",
    "name": "Device-test-age(更新)",
    "ip_address": "192.168.1.100",
    "os": "Linux",
    "status": "online",
    "last_heartbeat": "2026-01-27T10:00:00Z",
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T11:00:00Z"
  }
}
```

### 3. 隧道管理

#### 3.1 创建隧道

- **URL**: `/tunnels`
- **方法**: `POST`
- **请求体**:

```json
{
  "device_id": 1,
  "local_port": 8080,
  "remote_port": 80,
  "protocol": "tcp",
  "description": "Web服务隧道"
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "device_id": 1,
    "local_port": 8080,
    "remote_port": 80,
    "protocol": "tcp",
    "status": "created",
    "description": "Web服务隧道",
    "created_at": "2026-01-27T10:00:00Z",
    "updated_at": "2026-01-27T10:00:00Z"
  }
}
```

#### 3.2 获取所有隧道

- **URL**: `/tunnels`
- **方法**: `GET`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": [
    {
      "id": 1,
      "device_id": 1,
      "local_port": 8080,
      "remote_port": 80,
      "protocol": "tcp",
      "status": "created",
      "description": "Web服务隧道",
      "created_at": "2026-01-27T10:00:00Z",
      "updated_at": "2026-01-27T10:00:00Z"
    }
  ]
}
```

#### 3.3 获取设备的隧道

- **URL**: `/tunnels/device/{deviceId}`
- **方法**: `GET`
- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": [
    {
      "id": 1,
      "device_id": 1,
      "local_port": 8080,
      "remote_port": 80,
      "protocol": "tcp",
      "status": "created",
      "description": "Web服务隧道",
      "created_at": "2026-01-27T10:00:00Z",
      "updated_at": "2026-01-27T10:00:00Z"
    }
  ]
}
```

#### 3.4 更新隧道状态

- **URL**: `/tunnels/{id}/status`
- **方法**: `PUT`
- **请求体**:

```json
{
  "status": "running"
}
```

- **响应**:

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": 1,
    "status": "running"
  }
}
```

#### 3.5 删除隧道

- **URL**: `/tunnels/{id}`
- **方法**: `DELETE`
- **响应**:

```json
{
  "code": 200,
  "msg": "success"
}
```

### 4. WebSSH

#### 4.1 Web终端连接

- **URL**: `/devices/{id}/webterm`
- **方法**: `GET`
- **参数**:
  - `username`: 用户名
  - `password`: 密码
  - `port`: SSH端口（默认22）
  - `cols`: 终端列数（默认80）
  - `rows`: 终端行数（默认24）

- **响应**:

WebSocket连接，用于实时终端通信。

## 错误响应

当API请求失败时，返回以下格式的错误响应：

```json
{
  "code": 400,
  "msg": "error",
  "error": "Invalid request parameters"
}
```

## 状态码

| 状态码 | 描述 |
|-------|------|
| 200 | 请求成功 |
| 400 | 请求参数错误 |
| 401 | 未授权 |
| 403 | 禁止访问 |
| 404 | 资源不存在 |
| 500 | 服务器内部错误 |

## 示例代码

### 使用cURL调用API

#### 创建账户

```bash
curl -X POST http://localhost:7835/api/v1/accounts \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Linux通用",
    "username": "root",
    "password": "password123",
    "auth_type": "password",
    "description": "管理员账户",
    "is_active": true,
    "is_privileged": true
  }'
```

#### 获取所有设备

```bash
curl http://localhost:7835/api/v1/devices
```

### 使用JavaScript调用API

#### 创建隧道

```javascript
fetch('http://localhost:7835/api/v1/tunnels', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    device_id: 1,
    local_port: 8080,
    remote_port: 80,
    protocol: 'tcp',
    description: 'Web服务隧道'
  })
})
.then(response => response.json())
.then(data => console.log(data))
.catch(error => console.error('Error:', error));
```

## 版本历史

| 版本 | 日期 | 描述 |
|------|------|------|
| 1.0.0 | 2026-01-27 | 初始版本 |
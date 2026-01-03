# JWT + Redis 鉴权系统使用指南

本项目使用 JWT (JSON Web Token) 配合 Redis 实现了一个完整的用户认证和授权系统。

## 系统架构

### 核心组件

1. **JWT (JSON Web Token)**: 用于生成和验证用户令牌
2. **Redis**: 存储有效的 token 和用户会话信息
3. **中间件**: 提供路由级别的认证和授权

### 技术栈

- Go + Gin框架
- JWT: `github.com/golang-jwt/jwt/v5`
- Redis: `github.com/redis/go-redis/v9`

## 配置

在 `configs/config.yaml` 中配置 Redis 连接：

```yaml
redis:
  host: "192.168.200.132"
  port: 6389
  password: ""
  db: 0
  pool_size: 10
```

## Token 机制

### Token 特点

- **过期时间**: JWT token 有效期为 24 小时
- **Redis 存储**: Token 在 Redis 中的存储时间为 3 天（可续期）
- **双重验证**: 既验证 JWT 签名，也检查 Redis 中是否存在该 token
- **主动注销**: 支持通过删除 Redis 中的 token 实现登出

### Token 数据结构

JWT Claims 包含：
- `user_id`: 用户ID
- `email`: 用户邮箱
- `role`: 用户角色（0=普通用户，1=管理员）
- `exp`: 过期时间

### Redis Key 设计

```
auth:token:{token} -> userID
auth:session:{userID} -> session data (hash)
```

## API 使用

### 1. OAuth 登录

**Google 登录**:
```bash
GET /auth/google/login
```
重定向到 Google OAuth 页面

**回调处理**:
```bash
GET /auth/google/callback
```
- 自动创建或更新用户
- 生成 JWT token
- 将 token 存入 Redis（3天过期）
- 返回 token

响应示例：
```json
{
  "code": 200,
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expire": "72h0m0s"
}
```

### 2. 受保护的API调用

在请求头中包含 token：

```bash
Authorization: Bearer {token}
```

示例：
```bash
curl -X GET http://localhost:8888/api/profile \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIs..."
```

### 3. 获取当前用户信息

```bash
GET /auth/me
Authorization: Bearer {token}
```

响应：
```json
{
  "code": 200,
  "user_id": 1,
  "email": "user@example.com",
  "role": 0
}
```

### 4. 登出

```bash
POST /auth/logout
Authorization: Bearer {token}
```

从 Redis 中删除 token，使其失效。

## 中间件使用

### 1. 基础认证中间件

保护需要登录的路由：

```go
import "interestBar/pkg/server/middleware"

protected := r.Group("/api")
protected.Use(middleware.AuthMiddleware())
{
    protected.GET("/profile", userCtrl.GetProfile)
    protected.POST("/update", userCtrl.UpdateProfile)
}
```

### 2. 可选认证中间件

允许匿名访问，但如果提供了 token 也会验证：

```go
optional := r.Group("/public")
optional.Use(middleware.OptionalAuthMiddleware())
{
    optional.GET("/content", ctrl.GetContent)
}
```

### 3. 角色权限中间件

要求特定角色才能访问：

```go
admin := r.Group("/admin")
admin.Use(middleware.AuthMiddleware(), middleware.RoleMiddleware(1))
{
    admin.GET("/dashboard", adminCtrl.Dashboard)
    admin.DELETE("/users/:id", adminCtrl.DeleteUser)
}
```

角色说明：
- `0`: 普通用户
- `1`: 管理员
- 更高数字表示更高权限

### 4. Token 续期中间件

自动延长 token 过期时间（滑动窗口）：

```go
api := r.Group("/api")
api.Use(middleware.AuthMiddleware(), middleware.RefreshTokenMiddleware())
{
    api.GET("/data", ctrl.GetData)
}
```

每次请求后，token 在 Redis 中的过期时间会延长 3 天。

## 在控制器中获取用户信息

认证后，可以在控制器中从 context 获取用户信息：

```go
func (ctrl *UserController) UpdateProfile(c *gin.Context) {
    // 获取用户信息
    userID, exists := c.Get("user_id")
    if !exists {
        c.JSON(401, gin.H{"error": "Not authenticated"})
        return
    }

    email, _ := c.Get("email")
    role, _ := c.Get("role")

    // 使用用户信息...
    c.JSON(200, gin.H{
        "user_id": userID,
        "email": email,
        "role": role,
    })
}
```

## Redis 工具函数

```go
import "interestBar/pkg/server/storage/cache/redis"

// 存储 token（3天过期）
redis.SetToken(token, userID, 3*24*time.Hour)

// 获取 token 对应的用户ID
userID, err := redis.GetToken(token)

// 删除 token（登出）
redis.DeleteToken(token)

// 存储用户会话
sessionData := map[string]interface{}{
    "user_id": userID,
    "email": email,
    "role": role,
}
redis.SetUserSession(userID, sessionData, 3*24*time.Hour)

// 获取用户会话
session, err := redis.GetUserSession(userID)

// 删除用户会话
redis.DeleteUserSession(userID)
```

## JWT 工具函数

```go
import "interestBar/pkg/util"

// 生成 token
token, err := util.GenerateToken(userID, email, role)

// 解析 token
claims, err := util.ParseToken(token)

// 生成绑定 token（用于注册绑定）
bindingToken, err := util.GenerateBindingToken("google", googleID, email)
```

## 安全最佳实践

1. **HTTPS**: 生产环境必须使用 HTTPS 传输 token
2. **Token 存储**: 前端应将 token 存储在 httpOnly cookie 或 localStorage 中
3. **定期更换密钥**: 定期更换 JWT 密钥（`util.JwtSecret`）
4. **密码保护**: 生产环境 Redis 应设置密码
5. **Token 过期**: 合理设置 token 过期时间
6. **错误处理**: 不要在错误消息中暴露敏感信息

## 完整示例

### 创建一个受保护的 API

```go
// 1. 在路由中注册
api := r.Group("/api/v1")
api.Use(middleware.AuthMiddleware())
{
    api.GET("/posts", postCtrl.ListPosts)
    api.POST("/posts", postCtrl.CreatePost)
}

// 2. 在控制器中使用
func (ctrl *PostController) CreatePost(c *gin.Context) {
    userID, _ := c.Get("user_id")

    post := model.Post{
        UserID: userID.(uint),
        Title: c.PostForm("title"),
        Content: c.PostForm("content"),
    }

    if err := db.Create(&post).Error; err != nil {
        c.JSON(500, gin.H{"error": "Failed to create post"})
        return
    }

    c.JSON(200, gin.H{
        "code": 200,
        "message": "Post created successfully",
        "data": post,
    })
}
```

### 创建一个管理员 API

```go
admin := r.Group("/admin/v1")
admin.Use(middleware.AuthMiddleware(), middleware.RoleMiddleware(1))
{
    admin.GET("/users", adminCtrl.ListUsers)
    admin.DELETE("/users/:id", adminCtrl.DeleteUser)
}
```

## 测试

### 测试登录流程

```bash
# 1. 发起 Google 登录
curl http://localhost:8888/auth/google/login

# 2. 从回调中获取 token
# （浏览器会自动处理 OAuth 重定向）

# 3. 使用 token 访问受保护的 API
export TOKEN="your_jwt_token_here"
curl -X GET http://localhost:8888/api/profile \
  -H "Authorization: Bearer $TOKEN"

# 4. 获取当前用户信息
curl -X GET http://localhost:8888/auth/me \
  -H "Authorization: Bearer $TOKEN"

# 5. 登出
curl -X POST http://localhost:8888/auth/logout \
  -H "Authorization: Bearer $TOKEN"
```

## 故障排查

### Token 无效

- 检查 token 是否过期
- 检查 Redis 中是否存在该 token
- 检查 JWT 密钥是否一致

### Redis 连接失败

- 检查 Redis 服务是否启动
- 检查配置文件中的 Redis 地址和端口
- 检查网络连接

### 权限不足

- 检查用户角色是否满足要求
- 检查中间件的配置

## 扩展

### 添加新的认证方式

参考 `GoogleLogin` 和 `GoogleCallback` 实现：

1. 创建 OAuth 配置
2. 实现登录重定向
3. 实现回调处理
4. 生成 token 并存储到 Redis

### 自定义中间件

可以根据需要创建自定义中间件：

```go
func CustomMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 自定义逻辑
        c.Next()
    }
}
```

## 总结

这个鉴权系统提供了：
- ✅ JWT token 生成和验证
- ✅ Redis token 存储（3天过期）
- ✅ 中间件支持（认证、授权、续期）
- ✅ OAuth 登录（Google）
- ✅ 登出功能
- ✅ 角色权限控制
- ✅ 用户会话管理

简单易用，安全可靠！

# HTTP响应封装使用指南

本项目实现了统一的HTTP响应封装，规范API返回格式和状态码管理。

## 响应结构

### 标准响应结构

```json
{
  "code": 200,
  "message": "Success",
  "data": { }
}
```

### 分页响应结构

```json
{
  "code": 200,
  "message": "Success",
  "data": [ ],
  "total": 100,
  "page": 1,
  "per_page": 20
}
```

## 状态码定义

所有状态码定义在 [pkg/server/response/code.go](pkg/server/response/code.go)

### 成功状态码

| 状态码 | HTTP状态 | 说明 |
|--------|----------|------|
| `CodeSuccess (200)` | 200 | 请求成功 |

### 客户端错误 (4xx)

| 状态码 | HTTP状态 | 说明 |
|--------|----------|------|
| `CodeBadRequest (201)` | 400 | 错误的请求 |
| `CodeUnauthorized (202)` | 401 | 未授权 |
| `CodeForbidden (203)` | 403 | 禁止访问 |
| `CodeNotFound (204)` | 404 | 资源未找到 |
| `CodeMethodNotAllowed (205)` | 405 | 方法不允许 |
| `CodeRequestTimeout (206)` | 408 | 请求超时 |
| `CodeConflict (207)` | 409 | 资源冲突 |
| `CodeTooManyRequests (208)` | 429 | 请求过多 |
| `CodeValidationError (209)` | 400 | 验证失败 |

### 服务器错误 (5xx)

| 状态码 | HTTP状态 | 说明 |
|--------|----------|------|
| `CodeInternalError (210)` | 500 | 内部服务器错误 |
| `CodeNotImplemented (211)` | 501 | 未实现 |
| `CodeServiceUnavailable (212)` | 503 | 服务不可用 |

## 预定义消息

所有预定义消息在 [pkg/server/response/code.go](pkg/server/response/code.go)

### 成功消息

- `MsgSuccess` - 成功
- `MsgCreated` - 创建成功
- `MsgUpdated` - 更新成功
- `MsgDeleted` - 删除成功

### 错误消息

- `MsgBadRequest` - 错误的请求
- `MsgUnauthorized` - 需要授权
- `MsgInvalidToken` - 无效或过期的token
- `MsgForbidden` - 禁止访问
- `MsgNotFound` - 资源未找到
- `MsgInternalError` - 内部服务器错误
- `MsgDatabaseError` - 数据库错误
- `MsgRedisError` - 缓存错误
- `MsgInvalidCredentials` - 无效的凭证
- `MsgUserNotFound` - 用户未找到
- `MsgUserExists` - 用户已存在
- `MsgEmailAlreadyExists` - 邮箱已注册
- `MsgTokenRequired` - Token是必需的
- `MsgCSRFTokenRequired` - CSRF token是必需的
- `MsgInvalidCSRFToken` - 无效的CSRF token
- `MsgSessionExpired` - 会话已过期
- `MsgLoginRequired` - 请先登录
- `MsgPermissionDenied` - 权限不足
- ...更多见源文件

## 使用方法

### 导入包

```go
import "interestBar/pkg/server/response"
```

### 基础响应

#### 1. 成功响应

```go
// 返回数据
response.Success(c, gin.H{
    "user_id": 123,
    "email": "user@example.com",
})

// 不返回数据
response.Success(c, nil)
```

#### 2. 自定义消息的成功响应

```go
response.SuccessWithMessage(c, "登录成功", gin.H{
    "token": authToken,
})
```

#### 3. 创建成功 (201)

```go
response.Created(c, gin.H{
    "id": createdID,
})
```

### 错误响应

#### 1. 使用预定义消息

```go
// Bad Request (400)
response.BadRequest(c)

// Unauthorized (401)
response.Unauthorized(c)

// Forbidden (403)
response.Forbidden(c)

// Not Found (404)
response.NotFound(c)

// Validation Error (400)
response.ValidationError(c)

// Internal Error (500)
response.InternalError(c)

// Conflict (409)
response.Conflict(c, response.MsgEmailAlreadyExists)

// Too Many Requests (429)
response.TooManyRequests(c, response.MsgRateLimitExceeded)
```

#### 2. 自定义错误消息

```go
response.BadRequest(c, "用户名不能为空")
response.Unauthorized(c, "Token已过期，请重新登录")
response.Forbidden(c, "您没有权限执行此操作")
response.NotFound(c, "用户不存在")
response.InternalError(c, "数据库连接失败")
```

#### 3. 带数据的错误响应

```go
response.ErrorWithData(c, response.CodeValidationError, "验证失败", gin.H{
    "errors": []string{
        "邮箱格式不正确",
        "密码长度不能少于8位",
    },
})
```

### 分页响应

```go
// 标准分页
response.Pagination(c, userList, total, page, perPage)

// 自定义消息的分页
response.PaginationWithMessage(c, "查询成功", userList, total, page, perPage)
```

## 实际使用示例

### Controller示例

```go
package controller

import (
    "github.com/gin-gonic/gin"
    "interestBar/pkg/server/response"
    "interestBar/pkg/server/storage/db/pgsql"
    "interestBar/pkg/server/model"
)

type UserController struct{}

func (ctrl *UserController) GetUser(c *gin.Context) {
    userID := c.Param("id")

    var user model.SysUser
    if err := pgsql.DB.First(&user, userID).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            response.NotFound(c, "用户不存在")
            return
        }
        response.InternalError(c, response.MsgDatabaseError)
        return
    }

    response.Success(c, user)
}

func (ctrl *UserController) CreateUser(c *gin.Context) {
    var req struct {
        Email    string `json:"email" binding:"required,email"`
        Password string `json:"password" binding:"required,min=8"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        response.ValidationError(c, err.Error())
        return
    }

    // 检查邮箱是否已存在
    var existingUser model.SysUser
    if err := pgsql.DB.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
        response.Conflict(c, response.MsgEmailAlreadyExists)
        return
    }

    // 创建用户
    user := model.SysUser{
        Email: req.Email,
        // ... 其他字段
    }

    if err := pgsql.DB.Create(&user).Error; err != nil {
        response.InternalError(c, "创建用户失败")
        return
    }

    response.Created(c, gin.H{
        "id": user.ID,
        "email": user.Email,
    })
}

func (ctrl *UserController) ListUsers(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

    var users []model.SysUser
    var total int64

    pgsql.DB.Model(&model.SysUser{}).Count(&total)
    if err := pgsql.DB.Offset((page - 1) * perPage).Limit(perPage).Find(&users).Error; err != nil {
        response.InternalError(c)
        return
    }

    response.Pagination(c, users, total, page, perPage)
}

func (ctrl *UserController) UpdateUser(c *gin.Context) {
    userID := c.Param("id")

    // 检查用户是否存在
    var user model.SysUser
    if err := pgsql.DB.First(&user, userID).Error; err != nil {
        response.NotFound(c)
        return
    }

    var req struct {
        Username string `json:"username"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        response.BadRequest(c, response.MsgInvalidFormat)
        return
    }

    // 更新用户
    user.Username = req.Username
    if err := pgsql.DB.Save(&user).Error; err != nil {
        response.InternalError(c)
        return
    }

    response.SuccessWithMessage(c, response.MsgUpdated, user)
}

func (ctrl *UserController) DeleteUser(c *gin.Context) {
    userID := c.Param("id")

    // 检查用户是否存在
    var user model.SysUser
    if err := pgsql.DB.First(&user, userID).Error; err != nil {
        response.NotFound(c)
        return
    }

    // 删除用户（软删除）
    if err := pgsql.DB.Delete(&user).Error; err != nil {
        response.InternalError(c)
        return
    }

    response.SuccessWithMessage(c, response.MsgDeleted, nil)
}
```

### Middleware示例

```go
package middleware

import (
    "github.com/gin-gonic/gin"
    "interestBar/pkg/server/response"
)

func AdminOnly() gin.HandlerFunc {
    return func(c *gin.Context) {
        role, exists := c.Get("role")
        if !exists {
            response.Unauthorized(c, response.MsgLoginRequired)
            c.Abort()
            return
        }

        if role.(int) < 1 {
            response.Forbidden(c, response.MsgPermissionDenied)
            c.Abort()
            return
        }

        c.Next()
    }
}
```

## 中间件集成

所有中间件已更新为使用response封装：

- [middleware.Auth()](pkg/server/router/middleware/auth.go) - JWT认证
- [middleware.RoleAuth()](pkg/server/router/middleware/auth.go) - 角色权限
- [middleware.CSRF()](pkg/server/router/middleware/csrf.go) - CSRF保护
- [middleware.ValidateCSRFOrigin()](pkg/server/router/middleware/csrf.go) - Origin验证

## 响应格式对比

### 优化前

```go
c.JSON(http.StatusOK, gin.H{
    "code": 200,
    "message": "Success",
    "data": data,
})
```

### 优化后

```go
response.Success(c, data)
```

## 优势

1. **代码更简洁** - 减少重复代码
2. **类型安全** - 使用枚举而非魔法数字
3. **易于维护** - 统一修改响应格式
4. **消息统一** - 预定义常用错误消息
5. **自动化映射** - 自动将业务状态码映射到HTTP状态码
6. **易于扩展** - 轻松添加新的状态码和消息

## 最佳实践

### 1. 使用预定义消息

```go
// 推荐
response.Unauthorized(c, response.MsgInvalidToken)

// 不推荐
response.Unauthorized(c, "Token无效或已过期")
```

### 2. 一致的错误处理

```go
if err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        response.NotFound(c)
        return
    }
    response.InternalError(c)
    return
}
```

### 3. 提供有用的错误信息

```go
// 对于客户端错误
response.BadRequest(c, "用户名长度必须在3-20个字符之间")

// 对于服务器错误，不要暴露细节
response.InternalError(c)  // 使用通用消息
// 在日志中记录详细错误
logger.Log.Error("Failed to create user", zap.Error(err))
```

### 4. 数据验证

```go
func (ctrl *UserController) CreateUser(c *gin.Context) {
    var req CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.ValidationError(c, err.Error())
        return
    }

    // 业务逻辑...
}
```

## 扩展

### 添加新的状态码

在 [code.go](pkg/server/response/code.go) 中添加：

```go
const (
    // 现有状态码...
    CodePaymentRequired ResponseCode = 213 + iota
)

// 更新映射
var HTTPStatusMap = map[ResponseCode]int{
    // 现有映射...
    CodePaymentRequired: http.StatusPaymentRequired,
}

var CodeMessage = map[ResponseCode]string{
    // 现有消息...
    CodePaymentRequired: "Payment required",
}
```

### 添加新的响应函数

在 [response.go](pkg/server/response/response.go) 中添加：

```go
func PaymentRequired(c *gin.Context, message ...string) {
    msg := GetMessage(CodePaymentRequired)
    if len(message) > 0 && message[0] != "" {
        msg = message[0]
    }
    ErrorWithMessage(c, CodePaymentRequired, msg)
}
```

## 迁移指南

### 步骤1: 导入包

```go
import "interestBar/pkg/server/response"
```

### 步骤2: 替换响应

将所有的 `c.JSON()` 调用替换为对应的response函数：

```go
// Before
c.JSON(http.StatusOK, gin.H{
    "code": 200,
    "message": "Success",
    "data": data,
})

// After
response.Success(c, data)
```

### 步骤3: 更新错误处理

```go
// Before
c.JSON(http.StatusBadRequest, gin.H{
    "code": 400,
    "message": "Invalid input",
})

// After
response.BadRequest(c, "Invalid input")
```

### 步骤4: 更新导入

移除 `net/http` 导入（如果不再需要）：

```go
// Before
import (
    "net/http"
    "github.com/gin-gonic/gin"
)

// After
import (
    "github.com/gin-gonic/gin"
)
```

## 相关文件

- [pkg/server/response/code.go](pkg/server/response/code.go) - 状态码定义
- [pkg/server/response/response.go](pkg/server/response/response.go) - 响应函数
- [pkg/server/controller/user.go](pkg/server/controller/user.go) - 使用示例
- [pkg/server/router/middleware/auth.go](pkg/server/router/middleware/auth.go) - 中间件示例

# InterestBar

一个基于 Go 语言兴趣社区后端，类似百度贴吧/Reddit 社区

## 主要特性

### 🔐 认证与授权

- **Google OAuth 2.0 集成** - 支持第三方快速登录
- **Sa-Token 框架** - 轻量级权限认证框架
- **Token 会话管理** - 支持 3 天有效期，30 分钟活跃超时
- **基于角色的访问控制 (RBAC)** - 灵活的权限管理
- **CORS 支持** - 跨域请求安全控制

### 👤 用户管理

- 用户注册与登录（Google OAuth）
- 用户资料管理（用户名、邮箱、手机、头像等）
- 多第三方登录平台支持（Google 已实现，X/Twitter 和 GitHub 预留）
- 软删除功能
- Redis 缓存优化用户信息查询

### 🚀 API 设计

- RESTful API 风格
- 统一的响应格式与自定义状态码
- 分页支持
- 完善的错误处理机制
- 中间件支持（认证、CORS、CSRF 保护、日志记录）

## 技术栈

### 核心框架与库

- **[Gin](https://github.com/gin-gonic/gin)** v1.11.0 - HTTP Web 框架
- **[GORM](https://github.com/go-gorm/gorm)** v1.31.1 - ORM 数据库操作
- **[Sa-Token-Go](https://github.com/izhangzhihao/sa-token-go)** v0.1.7 - 认证鉴权框架
- **[PostgreSQL Driver](https://github.com/lib/pq)** v1.6.0 - PostgreSQL 数据库驱动
- **[Viper](https://github.com/spf13/viper)** v1.21.0 - 配置管理
- **[Zap](https://github.com/uber-go/zap)** v1.27.1 - 高性能日志库
- **[OAuth2](https://github.com/golang/oauth2)** v0.34.0 - OAuth 2.0 客户端实现

### 数据存储

- **PostgreSQL** - 主数据库
- **Redis** - 缓存与会话存储

## 项目结构

```
interestBar/
├── cmd/                    # 应用入口
│   ├── main.go             # 主程序启动文件
│   └── apps/
│       └── server.go       # 服务初始化与配置
├── pkg/                    # 内部包
│   ├── conf/               # 配置管理 (Viper)
│   ├── logger/             # 日志配置 (Zap)
│   ├── server/             # 核心业务逻辑
│   │   ├── auth/           # 认证模块
│   │   │   ├── google.go   # Google OAuth 集成
│   │   │   ├── sa_token_init.go
│   │   │   └── acl/        # 访问控制列表
│   │   ├── controller/     # API 控制器
│   │   ├── model/          # 数据模型
│   │   ├── response/       # HTTP 响应工具
│   │   ├── router/         # 路由定义与中间件
│   │   │   └── middleware/ # 中间件（认证、缓存、CORS、CSRF、日志）
│   │   └── storage/        # 存储层
│   │       ├── db/pgsql/   # PostgreSQL 连接
│   │       └── redis/      # Redis 缓存
│   └── util/               # 工具函数
├── configs/                # 配置文件
│   └── config.yaml         # 主配置文件
├── docs/                   # 文档
│   ├── db.md              # 数据库表结构
│   ├── response_summary.md # HTTP 响应系统说明
│   └── response_usage.md  # 响应使用指南
├── go.mod                  # Go 模块依赖
└── go.sum                  # 依赖校验和
```

## 快速开始

### 环境要求

- Go 1.25.4+
- PostgreSQL 17
- Redis 6+

### 1. 克隆项目

```bash
git clone https://github.com/yourusername/interestBar.git
cd interestBar
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 配置数据库

创建 PostgreSQL 数据库：

```sql
CREATE DATABASE interestbar;
```

数据库表结构请参考 [docs/db.md](docs/db.md)

### 4. 配置 Redis

确保 Redis 服务已启动，并修改 `configs/config.yaml` 中的连接配置。

### 5. 配置应用

编辑 `configs/config.yaml` 文件，配置以下关键信息：

```yaml
server:
  port: 8888
  mode: debug

oauth:
  google:
    client_id: "your-google-client-id"
    client_secret: "your-google-client-secret"
    redirect_url: "http://your-domain.com/auth/google/callback"
    frontend_redirect_url: "http://your-frontend.com/callback"

redis:
  host: "192.168.200.132"
  port: 6389
  password: ""
  db: 0

sa_token:
  token_name: "satoken"
  timeout: 259200 # 3 天
  active_timeout: 1800 # 30 分钟
```

### 6. 运行应用

```bash
go run cmd/main.go
```

或编译后运行：

```bash
go build -o interestBar.exe cmd/main.go
./interestBar.exe
```

服务将在 http://localhost:8888 启动

## API 端点

### 健康检查

- `GET /health` - 服务健康检查
- `GET /hello` - Hello World 测试端点

### 认证相关

- `GET /auth/google/login` - 跳转到 Google OAuth 登录
- `GET /auth/google/callback` - Google OAuth 回调处理
- `POST /auth/logout` - 用户登出
- `GET /auth/me` - 获取当前登录用户信息

### 用户管理

- `GET /user/get` - 获取用户资料（需认证）

详细的 API 文档请参考代码中的 [pkg/server/controller/](pkg/server/controller/) 目录。

## 认证流程

1. 用户点击 Google 登录
2. 重定向到 Google OAuth 授权页面
3. 用户授权后，回调创建或更新用户信息
4. Sa-Token 生成认证令牌
5. 用户被重定向到前端并携带令牌
6. 后续请求在请求头中携带令牌进行认证

请求头格式：

```
satoken: your-token-here
```

## 响应系统

项目实现了统一的 HTTP 响应系统，包含：

- 自定义状态码（200, 400-429, 500-503）
- 预定义错误消息（40+ 条）
- 一致的 JSON 响应格式
- 分页支持
- 类型安全的响应函数

响应格式示例：

```json
{
  "code": 200,
  "message": "success",
  "data": {...}
}
```

详细说明请参考 [docs/response_summary.md](docs/response_summary.md) 和 [docs/response_usage.md](docs/response_usage.md)

## 安全特性

- ✅ CORS 跨域保护
- ✅ CSRF 攻击防护
- ✅ Token 认证机制
- ✅ 基于角色的访问控制
- ✅ 安全会话管理
- ✅ 软删除数据保护

## 开发

### 代码规范

项目遵循 Go 语言常规代码规范：

- 使用 `gofmt` 格式化代码
- 遵循 Go 官方注释规范
- 使用有意义的变量和函数命名

### 添加新的 OAuth 提供商

1. 在 `pkg/server/auth/` 中创建新的 OAuth 文件（如 `github.go`）
2. 参照 `google.go` 实现 OAuth 流程
3. 在路由中添加相应的端点
4. 更新数据库用户表的 OAuth ID 字段

### 扩展用户模型

编辑 `pkg/server/model/user.go` 和数据库表结构，添加新字段。

## 配置说明

### CORS 配置

允许的前端源（在 `config.yaml` 中配置）：

- `https://l0sgai.github.io`
- `https://l0sgai.github.io/interestBar-frontend/`
- `http://localhost:*`
- `http://127.0.0.1:*`

### 缓存策略

- 使用 Redis 缓存用户信息
- 缓存过期时间：30 分钟
- 采用 Cache-Aside 模式
- 支持缓存失效

## 许可证

[MIT License](LICENSE)

## 贡献

欢迎提交 Issue 和 Pull Request！

## 联系方式

如有问题或建议，请提交 Issue 或联系维护者。

---

**注意**: 首次运行前请确保正确配置 `config.yaml` 中的所有必要参数，特别是 OAuth 凭证和数据库连接信息。

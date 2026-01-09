# AWS S3 集成快速开始指南

## 已完成的工作

✅ 安装了 AWS SDK v2 依赖包
✅ 更新了配置文件支持 S3 配置
✅ 创建了 S3 客户端封装（支持上传、下载、删除、预签名 URL 等）
✅ 创建了上传控制器（图片、视频、头像、多图上传）
✅ 编写完整的集成文档

## 快速开始

### 1. 设置环境变量

创建 `.env` 文件或直接设置环境变量：

```bash
# Windows (PowerShell)
$env:AWS_ACCESS_KEY_ID="your_access_key_id"
$env:AWS_SECRET_ACCESS_KEY="your_secret_access_key"
$env:AWS_REGION="us-east-1"
$env:S3_BUCKET_NAME="your_bucket_name"

# Windows (CMD)
set AWS_ACCESS_KEY_ID=your_access_key_id
set AWS_SECRET_ACCESS_KEY=your_secret_access_key
set AWS_REGION=us-east-1
set S3_BUCKET_NAME=your_bucket_name

# Linux/Mac
export AWS_ACCESS_KEY_ID="your_access_key_id"
export AWS_SECRET_ACCESS_KEY="your_secret_access_key"
export AWS_REGION="us-east-1"
export S3_BUCKET_NAME="your_bucket_name"
```

### 2. 在应用启动时初始化 S3 客户端

在 `cmd/main.go` 中添加：

```go
import (
    s3storage "interestBar/pkg/server/storage/s3"
)

func main() {
    // ... 其他初始化代码

    // 初始化 S3 客户端
    if err := s3storage.InitS3Client(); err != nil {
        logger.Log.Error("Failed to initialize S3 client", zap.Error(err))
        panic(err)
    }

    // ... 启动应用
}
```

### 3. 注册上传路由

在路由文件中添加上传相关的路由：

```go
uploadController := controller.NewUploadController(logger.Log)

uploadGroup := v1.Group("/upload")
{
    uploadGroup.POST("/image", uploadController.UploadImage)
    uploadGroup.POST("/video", uploadController.UploadVideo)
    uploadGroup.POST("/avatar", uploadController.UploadAvatar)
    uploadGroup.POST("/post-images", uploadController.UploadPostImages)
    uploadGroup.DELETE("/delete", uploadController.DeleteFile)
    uploadGroup.GET("/presign", uploadController.GetPresignedURL)
}
```

## 使用示例

### 直接在代码中使用 S3 客户端

```go
import s3storage "interestBar/pkg/server/storage/s3"

// 获取 S3 客户端
s3Client := s3storage.GetS3Client()

// 上传文件
key := s3storage.GenerateKeyWithUUID("images", "photo.jpg")
fileURL, err := s3Client.UploadFile(ctx, key, fileHeader, "public-read")

// 下载文件
data, contentType, err := s3Client.DownloadFile(ctx, key)

// 删除文件
err := s3Client.DeleteFile(ctx, key)

// 生成预签名 URL
url, err := s3Client.GetPresignedURL(ctx, key)
```

## API 端点

初始化完成后，可以使用以下 API：

- `POST /api/v1/upload/image` - 上传图片（最大 10MB）
- `POST /api/v1/upload/video` - 上传视频（最大 500MB）
- `POST /api/v1/upload/avatar` - 上传头像（最大 5MB）
- `POST /api/v1/upload/post-images` - 批量上传帖子图片（最多 9 张）
- `DELETE /api/v1/upload/delete?key=xxx` - 删除文件
- `GET /api/v1/upload/presign?key=xxx` - 获取预签名 URL

## 项目文件结构

```
interestBar/
├── pkg/
│   ├── conf/
│   │   └── conf.go                    # ✅ 添加了 S3 配置结构体
│   └── server/
│       ├── controller/
│       │   └── upload.go              # ✅ 新建上传控制器
│       └── storage/
│           └── s3/
│               ├── init.go            # ✅ S3 客户端初始化
│               └── s3.go              # ✅ S3 客户端封装
├── configs/
│   └── config.yaml                    # ✅ 添加了 S3 配置
├── docs/
│   └── s3_integration.md              # ✅ 完整集成文档
└── .env.example                       # ✅ 环境变量模板
```

## 常见问题

### 1. 如何使用 MinIO 替代 AWS S3？

在 `configs/config.yaml` 中设置 `endpoint`：

```yaml
s3:
  endpoint: "http://localhost:9000"  # MinIO 地址
```

### 2. 如何修改文件上传大小限制？

在控制器中修改 `ValidateFile` 调用的 maxSize 参数：

```go
// 例如改为 20MB
err := s3storage.ValidateFile(file, allowedExts, 20*1024*1024)
```

### 3. 如何自定义文件存储路径？

```go
// 自定义路径
key := s3storage.GenerateKey("custom/path", "filename.jpg")
// 结果: custom/path/2024/01/09/filename.jpg
```

## 下一步

1. 根据实际需求调整文件大小限制
2. 在路由中注册上传相关的端点
3. 在 main.go 中初始化 S3 客户端
4. 设置环境变量并测试上传功能

## 完整文档

查看 [docs/s3_integration.md](docs/s3_integration.md) 获取详细的 API 文档和使用示例。

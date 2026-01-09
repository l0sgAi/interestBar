# AWS S3 对象存储集成文档

## 概述

本项目已集成 AWS S3 对象存储服务，支持文件上传、下载、删除和预签名 URL 等功能。

## 安装依赖

依赖包已自动安装：
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/service/s3`
- `github.com/aws/aws-sdk-go-v2/credentials`

## 配置

### 1. 环境变量配置

在启动应用前，需要设置以下环境变量：

```bash
export AWS_ACCESS_KEY_ID="your_access_key_id"
export AWS_SECRET_ACCESS_KEY="your_secret_access_key"
export AWS_REGION="us-east-1"  # 或其他区域，如 ap-southeast-1
export S3_BUCKET_NAME="your_bucket_name"
```

### 2. 配置文件

配置文件已添加 S3 配置项到 `configs/config.yaml`：

```yaml
# AWS S3 对象存储配置
s3:
  access_key_id: "${AWS_ACCESS_KEY_ID}"           # AWS 访问密钥 ID
  secret_access_key: "${AWS_SECRET_ACCESS_KEY}"   # AWS 访问密钥
  region: "${AWS_REGION}"                         # AWS 区域
  bucket: "${S3_BUCKET_NAME}"                     # S3 存储桶名称
  endpoint: ""                                    # 可选: 自定义端点（MinIO）
  presign_url_expire: 3600                        # 预签名 URL 过期时间(秒)
```

### 3. 初始化 S3 客户端

在应用启动时初始化 S3 客户端：

```go
import s3storage "interestBar/pkg/server/storage/s3"

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

## 使用示例

### 1. 上传文件

#### 从 multipart.FileHeader 上传

```go
import s3storage "interestBar/pkg/server/storage/s3"

// 获取 S3 客户端
s3Client := s3storage.GetS3Client()

// 生成文件 Key
key := s3storage.GenerateKeyWithUUID("images", "photo.jpg")

// 上传文件
fileURL, err := s3Client.UploadFile(ctx, key, fileHeader, "public-read")
if err != nil {
    // 处理错误
}
```

#### 从字节数组上传

```go
data := []byte("file content")
contentType := "image/jpeg"

fileURL, err := s3Client.UploadFileFromBytes(ctx, key, data, contentType, "public-read")
```

#### 从 io.Reader 上传

```go
file, _ := os.Open("image.jpg")
defer file.Close()

fileURL, err := s3Client.UploadFileFromReader(ctx, key, file, "image/jpeg", "image.jpg", "public-read")
```

### 2. 下载文件

```go
data, contentType, err := s3Client.DownloadFile(ctx, key)
if err != nil {
    // 处理错误
}

// 使用下载的数据
fmt.Println("Content-Type:", contentType)
fmt.Println("Size:", len(data))
```

### 3. 删除文件

```go
// 删除单个文件
err := s3Client.DeleteFile(ctx, key)

// 批量删除
keys := []string{"file1.jpg", "file2.jpg"}
err := s3Client.DeleteMultipleFiles(ctx, keys)
```

### 4. 生成预签名 URL

```go
// 生成临时访问 URL（用于私有文件）
url, err := s3Client.GetPresignedURL(ctx, key)
// URL 在配置的过期时间内有效（默认 1 小时）
```

### 5. 检查文件是否存在

```go
exists, err := s3Client.CheckFileExists(ctx, key)
if exists {
    fmt.Println("File exists")
}
```

### 6. 列出文件

```go
// 列出指定前缀的所有文件
keys, err := s3Client.ListFiles(ctx, "images/", 100)
```

### 7. 复制文件

```go
// 在同一存储桶内复制文件
err := s3Client.CopyFile(ctx, "source/key.jpg", "destination/key.jpg")
```

## 工具函数

### 生成文件 Key

```go
// 简单生成（带时间戳路径）
key := s3storage.GenerateKey("images", "photo.jpg")
// 结果: images/2024/01/09/photo.jpg

// 生成唯一 Key（带 UUID）
key := s3storage.GenerateKeyWithUUID("images", "photo.jpg")
// 结果: images/2024/01/09/photo_20240109150405_abc123.jpg
```

### 文件验证

```go
// 验证文件类型和大小
allowedExts := []string{".jpg", ".png", ".gif"}
maxSize := int64(10 * 1024 * 1024) // 10MB

err := s3storage.ValidateFile(fileHeader, allowedExts, maxSize)
```

### 获取 Content-Type

```go
contentType := s3storage.GetContentType("photo.jpg") // "image/jpeg"
contentType := s3storage.GetContentType("video.mp4") // "video/mp4"
```

### 判断文件类型

```go
isImage := s3storage.IsImageFile("photo.jpg")   // true
isVideo := s3storage.IsVideoFile("video.mp4")   // true
```

### 获取公共 URL

```go
publicURL := s3Client.GetPublicURL(key)
// 返回: https://bucket.s3.region.amazonaws.com/key
```

## API 接口

项目提供了以下文件上传相关的 API 接口：

### 1. 上传图片
```
POST /api/v1/upload/image
Content-Type: multipart/form-data

参数:
  - file: 图片文件（支持 jpg, png, gif, webp）

响应:
{
  "code": 200,
  "data": {
    "url": "https://bucket.s3.region.amazonaws.com/...",
    "key": "images/2024/01/09/...",
    "filename": "photo.jpg",
    "size": 123456
  }
}
```

### 2. 上传视频
```
POST /api/v1/upload/video
Content-Type: multipart/form-data

参数:
  - file: 视频文件（支持 mp4, avi, mov, mkv, webm）

响应:
{
  "code": 200,
  "data": {
    "url": "https://bucket.s3.region.amazonaws.com/...",
    "key": "videos/2024/01/09/...",
    "filename": "video.mp4",
    "size": 12345678
  }
}
```

### 3. 上传头像
```
POST /api/v1/upload/avatar
Content-Type: multipart/form-data

参数:
  - file: 头像图片

响应:
{
  "code": 200,
  "data": {
    "url": "https://bucket.s3.region.amazonaws.com/...",
    "key": "avatars/user123/user123.jpg",
    "filename": "avatar.jpg",
    "size": 56789
  }
}
```

### 4. 上传帖子图片（批量）
```
POST /api/v1/upload/post-images
Content-Type: multipart/form-data

参数:
  - files: 图片文件（可多个，最多 9 张）

响应:
{
  "code": 200,
  "data": {
    "uploaded": 3,
    "total": 3,
    "images": [
      {
        "url": "https://...",
        "key": "...",
        "filename": "image1.jpg",
        "size": 123456
      },
      ...
    ]
  }
}
```

### 5. 删除文件
```
DELETE /api/v1/upload/delete?key=images/2024/01/09/photo.jpg

响应:
{
  "code": 200,
  "message": "File deleted successfully"
}
```

### 6. 获取预签名 URL
```
GET /api/v1/upload/presign?key=private/file.pdf

响应:
{
  "code": 200,
  "data": {
    "url": "https://bucket.s3.region.amazonaws.com/private/file.pdf?...",
    "key": "private/file.pdf"
  }
}
```

## 使用 MinIO 或其他 S3 兼容服务

如果需要使用 MinIO 或其他 S3 兼容服务，只需在配置文件中设置 `endpoint`：

```yaml
s3:
  access_key_id: "minioadmin"
  secret_access_key: "minioadmin"
  region: "us-east-1"
  bucket: "interestbar"
  endpoint: "http://localhost:9000"  # MinIO 端点
  presign_url_expire: 3600
```

## 权限说明

### ACL 选项

上传文件时可指定 ACL（访问控制列表）：

- `private`: 私有文件，只有所有者可以访问
- `public-read`: 公开读取，任何人可以读取
- `public-read-write`: 公开读写，任何人可以读取和写入
- `authenticated-read`: 已认证用户可读取

默认使用 `public-read`。

### 私有文件访问

对于私有文件（`private` ACL），使用预签名 URL 进行临时访问：

```go
url, err := s3Client.GetPresignedURL(ctx, "private/file.pdf")
```

## 错误处理

所有 S3 操作都会返回错误，建议进行适当的错误处理：

```go
fileURL, err := s3Client.UploadFile(ctx, key, file, "public-read")
if err != nil {
    // 检查错误类型
    if strings.Contains(err.Error(), "NoSuchBucket") {
        // 存储桶不存在
    } else if strings.Contains(err.Error(), "AccessDenied") {
        // 权限不足
    } else {
        // 其他错误
    }
    return
}
```

## 最佳实践

1. **使用路径组织文件**
   ```go
   // 推荐：使用有意义的路径
   key := s3storage.GenerateKey("users/123/avatars", "avatar.jpg")
   // 结果: users/123/avatars/2024/01/09/avatar.jpg
   ```

2. **文件名唯一性**
   ```go
   // 推荐：使用带 UUID 的函数避免文件名冲突
   key := s3storage.GenerateKeyWithUUID("images", "photo.jpg")
   ```

3. **限制文件大小**
   ```go
   // 图片限制 10MB
   err := s3storage.ValidateFile(file, allowedExts, 10*1024*1024)
   ```

4. **设置合适的 Content-Type**
   ```go
   contentType := s3storage.GetContentType(filename)
   fileURL, err := s3Client.UploadFileFromBytes(ctx, key, data, contentType, "public-read")
   ```

5. **使用日志记录**
   ```go
   // S3 客户端会自动记录操作日志，便于调试
   logger.Log.Info("File uploaded", zap.String("url", fileURL))
   ```

## 注意事项

1. **环境变量**: 确保在启动应用前设置所有必需的环境变量
2. **IAM 权限**: 确保 AWS 凭证具有足够的 S3 权限
3. **存储桶策略**: 如果使用 `public-read`，确保存储桶策略允许公开读取
4. **成本监控**: 注意监控 S3 使用量，避免意外产生高额费用
5. **HTTPS**: 生产环境建议使用 HTTPS 访问 S3

## 故障排查

### 初始化失败

```
Failed to load AWS config: invalid credentials
```
**解决方案**: 检查 `AWS_ACCESS_KEY_ID` 和 `AWS_SECRET_ACCESS_KEY` 是否正确

### 上传失败

```
Failed to upload file to S3: NoSuchBucket
```
**解决方案**: 检查存储桶名称是否正确，或创建存储桶

```
Failed to upload file to S3: AccessDenied
```
**解决方案**: 检查 IAM 用户权限，确保有 `s3:PutObject` 权限

### 网络问题

如果使用 MinIO 或其他兼容服务，确保：
1. `endpoint` 配置正确
2. 网络可访问
3. 防火墙/安全组允许访问

## 相关文件

- [S3 客户端实现](../pkg/server/storage/s3/s3.go)
- [S3 初始化](../pkg/server/storage/s3/init.go)
- [上传控制器](../pkg/server/controller/upload.go)
- [配置文件](../configs/config.yaml)

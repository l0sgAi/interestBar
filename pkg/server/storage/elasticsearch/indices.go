package elasticsearch

import (
	"fmt"
	"interestBar/pkg/conf"
)

// Elasticsearch 索引名称常量
const (
	// IndexCircle 圈子索引名称
	IndexCircle = "circle"
	// IndexPost 帖子索引名称
	IndexPost = "post"
	// IndexUser 用户索引名称（预留）
	IndexUser = "users"
	// IndexComment 评论索引名称（预留）
	IndexComment = "comment"
)

// GetIndexName 获取完整的 Elasticsearch 索引名称
// 格式: {prefix}.{entity}
// 例如: pg.public.circle, pg.public.post
func GetIndexName(entity string) string {
	prefix := conf.Config.Elasticsearch.IndexPrefix
	if prefix == "" {
		// 如果没有配置前缀，直接使用实体名称
		return entity
	}
	return fmt.Sprintf("%s.%s", prefix, entity)
}

// GetCircleIndexName 获取圈子索引名称
func GetCircleIndexName() string {
	return GetIndexName(IndexCircle)
}

// GetPostIndexName 获取帖子索引名称
func GetPostIndexName() string {
	return GetIndexName(IndexPost)
}

// GetUserIndexName 获取用户索引名称
func GetUserIndexName() string {
	return GetIndexName(IndexUser)
}

// GetCommentIndexName 获取评论索引名称
func GetCommentIndexName() string {
	return GetIndexName(IndexComment)
}

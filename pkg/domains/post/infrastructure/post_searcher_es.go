package infrastructure

import (
	"context"
	"encoding/json"

	"interestBar/pkg/domains/post/application"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"

	"github.com/google/uuid"
)

// postSearcherES 基于 pkg/server/storage/elasticsearch 的 PostSearcher 实现。
type postSearcherES struct{}

// NewPostSearcher 构造 PostSearcher。
func NewPostSearcher() application.PostSearcher {
	return &postSearcherES{}
}

// Search 搜索帖子（circleID 为 uuid.Nil 时搜索所有圈子）。
func (s *postSearcherES) Search(ctx context.Context, keyword string, circleID uuid.UUID, size int, searchAfter []interface{}) (*application.RawPostSearchResult, error) {
	result, err := elasticsearch.SearchPosts(keyword, circleID, size, searchAfter)
	if err != nil {
		return nil, err
	}

	posts := make([]application.PostDoc, 0, len(result.Posts))
	for _, doc := range result.Posts {
		posts = append(posts, application.PostDoc{
			ID: doc.ID, CircleID: doc.CircleID, UserID: doc.UserID, Type: doc.Type,
			Title: doc.Title, Summary: doc.Summary, Content: doc.Content,
			ViewCount: doc.ViewCount, CommentCount: doc.CommentCount,
			LikeCount: doc.LikeCount, CollectCount: doc.CollectCount,
			IsPinned: doc.IsPinned, IsEssence: doc.IsEssence, IsLock: doc.IsLock,
			Status: doc.Status, CreateTime: doc.CreateTime,
		})
	}

	return &application.RawPostSearchResult{
		Posts:       posts,
		Total:       result.Total,
		Size:        result.Size,
		SearchAfter: marshalSearchAfter(result.SearchAfter),
	}, nil
}

// marshalSearchAfter 把 []interface{} 序列化为 JSON 字符串。
func marshalSearchAfter(arr []interface{}) string {
	if arr == nil {
		return ""
	}
	if bytes, err := json.Marshal(arr); err == nil {
		return string(bytes)
	}
	return ""
}

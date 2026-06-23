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
	return toRawPostSearchResult(result), nil
}

// SearchMy 搜索指定用户自己的帖子（keyword 为空时返回该用户全部帖子）。
func (s *postSearcherES) SearchMy(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*application.RawPostSearchResult, error) {
	result, err := elasticsearch.SearchMyPosts(userID, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}
	return toRawPostSearchResult(result), nil
}

// SearchByUser 搜索指定用户已发布的帖子（查看「他人」发帖用，强制 status=1）。
func (s *postSearcherES) SearchByUser(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*application.RawPostSearchResult, error) {
	result, err := elasticsearch.SearchUserPosts(userID, keyword, size, searchAfter)
	if err != nil {
		return nil, err
	}
	return toRawPostSearchResult(result), nil
}

// toRawPostSearchResult 把 ES PostListResponse 转为 application.RawPostSearchResult。
func toRawPostSearchResult(r *elasticsearch.PostListResponse) *application.RawPostSearchResult {
	return &application.RawPostSearchResult{
		Posts:       toPostDocs(r.Posts),
		Total:       r.Total,
		Size:        r.Size,
		SearchAfter: marshalSearchAfter(r.SearchAfter),
	}
}

// toPostDocs 把 ES PostDocument 列表转为 application.PostDoc 列表。
func toPostDocs(docs []elasticsearch.PostDocument) []application.PostDoc {
	posts := make([]application.PostDoc, 0, len(docs))
	for _, doc := range docs {
		posts = append(posts, application.PostDoc{
			ID: doc.ID, CircleID: doc.CircleID, UserID: doc.UserID, Type: doc.Type,
			Title: doc.Title, Summary: doc.Summary, Content: doc.Content,
			ViewCount: doc.ViewCount, CommentCount: doc.CommentCount,
			LikeCount: doc.LikeCount, CollectCount: doc.CollectCount,
			IsPinned: doc.IsPinned, IsEssence: doc.IsEssence, IsLock: doc.IsLock,
			Status: doc.Status, CreateTime: doc.CreateTime,
		})
	}
	return posts
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

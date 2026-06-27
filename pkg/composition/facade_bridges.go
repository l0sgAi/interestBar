// Package composition 的 facade_bridges.go：
// 跨领域 Facade 桥接器，把各领域的 Facade 接口连起来。
//
// 这是"领域间通过 Facade 通信"模式的核心：
//   - user 领域暴露 UserFacade（GetBriefs/GetBrief）
//   - circle 领域暴露 CircleFacade（GetBriefs）+ CircleMemberChecker + CircleStatusChecker + CirclePostCountPort
//   - post 领域暴露 PostMediaFetcher（GetMediaByPostIDs）
//
// 各领域定义的 Facade 接口字段类型（如 user.application.UserBrief vs
// circle.application.UserBrief）虽然结构相同但是不同类型，
// 因此桥接器需要做字段级转换。
package composition

import (
	"context"

	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	commentapp "interestBar/pkg/domains/comment/application"
	historyapp "interestBar/pkg/domains/history/application"
	postapp "interestBar/pkg/domains/post/application"
	userapp "interestBar/pkg/domains/user/application"

	"github.com/google/uuid"
)

// ===== user → (circle, post) =====

// circleUserFacade 把 user.application.UserFacade 适配为 circle.application.UserFacade。
type circleUserFacade struct {
	delegate userapp.UserFacade
}

func (f *circleUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]circleapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]circleapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = circleapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// postUserFacade 把 user.application.UserFacade 适配为 post.application.UserFacade。
type postUserFacade struct {
	delegate userapp.UserFacade
}

func (f *postUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]postapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]postapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = postapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

func (f *postUserFacade) GetBrief(ctx context.Context, userID string) (*postapp.UserBrief, error) {
	b, err := f.delegate.GetBrief(ctx, userID)
	if err != nil || b == nil {
		return nil, err
	}
	return &postapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}, nil
}

// ===== circle → post =====

// postCircleFacade 把 circle.application.CircleFacade 适配为 post.application.CircleFacade。
type postCircleFacade struct {
	delegate circleapp.CircleFacade
}

func (f *postCircleFacade) GetBriefs(ctx context.Context, circleIDs []string) (map[string]postapp.CircleBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, circleIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]postapp.CircleBrief, len(briefs))
	for id, b := range briefs {
		result[id] = postapp.CircleBrief{ID: b.ID, Name: b.Name, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

// postMediaFetcherForCircle 把 post.application.PostService 适配为
// circle.application.PostMediaFetcher。
type postMediaFetcherForCircle struct {
	delegate postapp.PostService
}

func (f *postMediaFetcherForCircle) GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error) {
	return f.delegate.GetMediaByPostIDs(ctx, postIDs)
}

// ===== post → circle（成员/状态校验 + 帖子计数端口）=====

// circleMemberCheckerForPost 把 circle MemberRepository 适配为 post 的 CircleMemberChecker。
type circleMemberCheckerForPost struct {
	memberRepo circledomain.MemberRepository
}

func (c *circleMemberCheckerForPost) GetMember(ctx context.Context, circleID, userID uuid.UUID) (*postapp.CircleMemberInfo, error) {
	m, err := c.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil {
		if err == circledomain.ErrMemberNotFound {
			return nil, nil
		}
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	return &postapp.CircleMemberInfo{
		Role:        m.Role,
		Status:      m.Status,
		MuteEndTime: m.MuteEndTime,
	}, nil
}

// circleStatusCheckerForPost 把 circle CircleRepository 适配为 post 的 CircleStatusChecker。
type circleStatusCheckerForPost struct {
	circleRepo circledomain.CircleRepository
}

func (c *circleStatusCheckerForPost) IsCircleAvailable(ctx context.Context, circleID uuid.UUID) (bool, error) {
	circle, err := c.circleRepo.GetByID(ctx, circleID)
	if err != nil {
		if err == circledomain.ErrCircleNotFound {
			return false, nil
		}
		return false, err
	}
	return circle.Status == circledomain.CircleStatusNormal, nil
}

// circlePostCountPortForPost 把 circle CircleService 适配为 post 的 CirclePostCountPort。
type circlePostCountPortForPost struct {
	svc circleapp.CircleService
}

func (p *circlePostCountPortForPost) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	return p.svc.IncrPostCount(ctx, circleID)
}

// ===== user → comment =====

// commentUserFacade 把 user.application.UserFacade 适配为 comment.application.UserFacade。
type commentUserFacade struct {
	delegate userapp.UserFacade
}

func (f *commentUserFacade) GetBriefs(ctx context.Context, userIDs []string) (map[string]commentapp.UserBrief, error) {
	briefs, err := f.delegate.GetBriefs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[string]commentapp.UserBrief, len(briefs))
	for id, b := range briefs {
		result[id] = commentapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
	}
	return result, nil
}

func (f *commentUserFacade) GetBrief(ctx context.Context, userID string) (*commentapp.UserBrief, error) {
	b, err := f.delegate.GetBrief(ctx, userID)
	if err != nil || b == nil {
		return nil, err
	}
	return &commentapp.UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}, nil
}

// ===== post → comment（帖子元信息 + 评论计数端口）=====

// commentPostLookup 把 post.application.PostService 适配为 comment.application.PostLookup。
//
// comment 领域发评论时需要：
//   - 校验帖子存在性/状态/锁定 → GetPost
//   - 恢复帖子统计缓存 + 递增评论计数 → RestoreStatsAndIncrCommentCount
type commentPostLookup struct {
	delegate postapp.PostService
}

func (l *commentPostLookup) GetPost(ctx context.Context, postID uuid.UUID) (*commentapp.PostInfo, error) {
	meta, err := l.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return nil, err
	}
	return &commentapp.PostInfo{
		ID:     meta.ID,
		Status: meta.Status,
		IsLock: meta.IsLock,
	}, nil
}

func (l *commentPostLookup) RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	return l.delegate.RestoreStatsAndIncrCommentCount(ctx, postID)
}

// ===== post → like（帖子存在性 + 统计缓存恢复）=====

// likePostTarget 把 post.application.PostService 适配为 like.application.PostTarget。
type likePostTarget struct {
	delegate postapp.PostService
}

func (t *likePostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return true, nil
}

func (t *likePostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

// ===== comment → like（评论存在性 + 所属帖子ID + 统计缓存恢复）=====

// likeCommentTarget 把 comment.application.CommentService 适配为 like.application.CommentTarget。
type likeCommentTarget struct {
	delegate commentapp.CommentService
}

func (t *likeCommentTarget) ExistsWithPostID(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, bool, error) {
	postID, err := t.delegate.GetCommentMeta(ctx, commentID)
	if err != nil {
		return nil, false, err
	}
	if postID == nil {
		return nil, false, nil
	}
	return postID, true, nil
}

func (t *likeCommentTarget) RestoreStats(ctx context.Context, commentID uuid.UUID) error {
	return t.delegate.RestoreCommentStats(ctx, commentID)
}

// ===== post → collect（帖子存在性 + 统计缓存恢复 + 列表组装）=====

// collectPostTarget 把 post.application.PostService 适配为 collect.domain.PostTarget。
//
// 与 likePostTarget 行为完全一致（collect.PostTarget 与 like.PostTarget 签名相同），
// 独立定义以保持命名清晰、避免跨领域类型耦合。
type collectPostTarget struct {
	delegate postapp.PostService
}

func (t *collectPostTarget) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	meta, err := t.delegate.GetPostMeta(ctx, postID)
	if err != nil || meta == nil {
		return false, err
	}
	return true, nil
}

func (t *collectPostTarget) RestoreStats(ctx context.Context, postID uuid.UUID) error {
	return t.delegate.RestoreStats(ctx, postID)
}

// collectPostFetcher 把 post.application.PostService 适配为 collect.application.PostFetcher。
type collectPostFetcher struct {
	delegate postapp.PostService
}

func (f *collectPostFetcher) GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.GetPostsByIDs(ctx, postIDs)
}

// ===== history ↔ post =====

// postHistoryRecorder 把 history.application.HistoryService 适配为 post.domain.HistoryRecorder。
// 供 post 域详情页浏览时 async 回调记录浏览历史。
type postHistoryRecorder struct {
	delegate historyapp.HistoryService
}

func (r *postHistoryRecorder) RecordView(ctx context.Context, userID, postID uuid.UUID) error {
	return r.delegate.RecordView(ctx, userID, postID)
}

// historyPostFetcher 把 post.application.PostService 适配为 history.application.PostFetcher(ES 来源)。
// 供 history「最近浏览」列表按 postID 从 ES 批量取已组装帖子。
type historyPostFetcher struct {
	delegate postapp.PostService
}

func (f *historyPostFetcher) SearchByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error) {
	return f.delegate.SearchPostsByIDs(ctx, postIDs)
}

func (f *historyPostFetcher) SearchByIDsAndKeyword(ctx context.Context, postIDs []uuid.UUID, keyword string, size, offset int) ([]postapp.PostListItem, int64, error) {
	return f.delegate.SearchPostsByIDsAndKeyword(ctx, postIDs, keyword, size, offset)
}


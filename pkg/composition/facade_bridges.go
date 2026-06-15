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

// Package application 提供 user 领域的应用服务层。
//
// 职责：
//   - 用例编排（用户 CRUD、搜索、缓存策略、资料更新）；
//   - 通过 UserFacade 接口向其他领域暴露"用户精简视图"查询能力。
//
// 跨领域通信约定：post/comment/circle 等领域需要用户信息时，
// 依赖本包的 UserFacade 接口，由 composition 注入实现，避免直接 import
// user 领域的 domain/infrastructure 包。
package application

import (
	"context"
	"strings"
	"time"

	"interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
)

// UserBrief 是给跨领域调用的用户精简视图（与 domain.UserBrief 对应）。
//
// 定义在 application 层而非 domain 层，是因为"对外暴露什么字段"是用例决策，
// domain.UserBrief 是纯粹的值对象，application.UserBrief 是契约。
type UserBrief struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// UserFacade 是 user 领域给其他领域的跨领域查询接口。
//
// 任何需要"组装用户信息"的领域（post/comment/circle）都应依赖此接口，
// 而非直接查询数据库。这样：
//   - 解耦：被调用方不感知 user 的存储实现；
//   - 可测：mock UserFacade 即可测试组装逻辑；
//   - 未来拆服务：把 UserFacade 实现换成 RPC client 即可。
type UserFacade interface {
	// GetBriefs 批量获取用户精简视图。入参为 UUIDv7 字符串列表，
	// 返回以 ID 字符串为 key 的 map。未找到的用户不会出现在 map 里。
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	// GetBrief 获取单个用户精简视图。未找到返回 nil, nil。
	GetBrief(ctx context.Context, userID string) (*UserBrief, error)
}

// UserListItemVO 用户列表项（搜索结果用）。
type UserListItemVO struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	AvatarURL  string `json:"avatar_url"`
	Gender     int8   `json:"gender"`
	Role       int8   `json:"role"`
	CreateTime string `json:"create_time"`
}

// UserSearchResult 用户搜索结果。
type UserSearchResult struct {
	Total       int64           `json:"total"`
	Size        int             `json:"size"`
	SearchAfter string          `json:"search_after"`
	Users       []UserListItemVO `json:"data"`
}

// UpdateProfileInput 修改资料的入参（指针字段允许部分更新）。
type UpdateProfileInput struct {
	Username  *string
	AvatarURL *string
	Phone     *string
	Gender    *int
	Birthdate *time.Time
}

// UpdateProfileResult 修改资料的返回值。
type UpdateProfileResult struct {
	ID         uuid.UUID `json:"id"`
	Username   string    `json:"username"`
	Email      string    `json:"email"`
	AvatarURL  string    `json:"avatar_url"`
	Phone      string    `json:"phone"`
	Gender     int       `json:"gender"`
	Birthdate  *time.Time `json:"birthdate"`
}

// UserSearcher 是用户搜索的抽象（由 infrastructure 提供 ES 实现）。
//
// 抽象出来是为了让 UserService 依赖接口而非直接 import ES 包。
type UserSearcher interface {
	// Search 按关键字搜索用户，支持 search_after 分页。
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*UserSearchResult, error)
}

// UserService 是 user 领域的应用服务接口。
type UserService interface {
	// GetByID 获取用户详情（带缓存）。
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.SysUser, error)
	// GetByIDStr 获取用户详情，入参为 loginID 字符串（/user/get 等会话接口用）。
	GetByIDStr(ctx context.Context, loginID string) (*domain.SysUser, error)
	// GetByEmail 按邮箱查询（无缓存，登录/注册流程用）。
	GetByEmail(ctx context.Context, email string) (*domain.SysUser, error)
	// UpdateProfile 修改用户资料（部分字段更新）。
	UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*UpdateProfileResult, error)
	// Search 搜索用户。searchAfter 为已解析的分页游标（可为 nil）。
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*UserSearchResult, error)
}

type userServiceImpl struct {
	repo    domain.UserRepository
	cache   domain.UserCache
	searcher UserSearcher
}

// NewUserService 构造一个 UserService。
func NewUserService(repo domain.UserRepository, cache domain.UserCache, searcher UserSearcher) UserService {
	return &userServiceImpl{repo: repo, cache: cache, searcher: searcher}
}

// NewUserFacade 从 UserService 构造一个 UserFacade。
//
// 通常在 composition 层调用：先把 userService 构造好，
// 再用 NewUserFacade(svc) 产出注入给 post/circle 等领域的 facade 实例。
func NewUserFacade(svc UserService) UserFacade {
	return &userFacadeAdapter{svc: svc}
}

// userFacadeAdapter 把 UserService 适配为 UserFacade。
type userFacadeAdapter struct {
	svc UserService
}

// GetBriefs 批量获取用户精简视图。
func (f *userFacadeAdapter) GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error) {
	if len(userIDs) == 0 {
		return make(map[string]UserBrief), nil
	}

	ids := make([]uuid.UUID, 0, len(userIDs))
	for _, s := range userIDs {
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return make(map[string]UserBrief), nil
	}

	// 这里复用 GetByID（带缓存），避免一次性拉全表。
	// 若后续需要更高效的批量查询，可在 UserService 增加 GetByIDs 方法。
	// 当前优先保证缓存命中率，与旧 controller 行为一致。
	result := make(map[string]UserBrief, len(ids))
	for _, id := range ids {
		user, err := f.svc.GetByID(ctx, id)
		if err != nil || user == nil {
			continue
		}
		if user.Status != domain.UserStatusActive || user.Deleted != domain.UserNotDeleted {
			continue
		}
		result[id.String()] = UserBrief{
			ID:        user.ID.String(),
			Username:  user.Username,
			AvatarURL: user.AvatarURL,
		}
	}
	return result, nil
}

// GetBrief 获取单个用户精简视图。
func (f *userFacadeAdapter) GetBrief(ctx context.Context, userID string) (*UserBrief, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, nil
	}
	user, err := f.svc.GetByID(ctx, id)
	if err != nil || user == nil {
		return nil, err
	}
	if user.Status != domain.UserStatusActive || user.Deleted != domain.UserNotDeleted {
		return nil, nil
	}
	return &UserBrief{
		ID:        user.ID.String(),
		Username:  user.Username,
		AvatarURL: user.AvatarURL,
	}, nil
}

// GetByID 获取用户详情，先查缓存再回源 DB。
//
// 行为与旧 controller.GetUserDetail 一致：
//   1. 先查缓存（命中且 status=1, deleted=0 直接返回）；
//   2. 缓存未命中查 DB；
//   3. 校验 status/deleted；
//   4. 回写缓存（失败不影响主流程）。
func (s *userServiceImpl) GetByID(ctx context.Context, userID uuid.UUID) (*domain.SysUser, error) {
	// 1. 缓存
	if cached, _ := s.cache.GetUser(ctx, userID); cached != nil {
		if cached.Status != domain.UserStatusActive || cached.Deleted != domain.UserNotDeleted {
			return nil, nil
		}
		return cached, nil
	}

	// 2. DB
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	// 3. 校验
	if user.Status != domain.UserStatusActive || user.Deleted != domain.UserNotDeleted {
		return nil, nil
	}

	// 4. 回写缓存
	_ = s.cache.SetUser(ctx, userID, user)

	return user, nil
}

// GetByEmail 按邮箱查询（无缓存）。
func (s *userServiceImpl) GetByEmail(ctx context.Context, email string) (*domain.SysUser, error) {
	return s.repo.GetByEmail(ctx, email)
}

// GetByIDStr 接收 loginID 字符串，先 parse 成 uuid 再走 GetByID。
// 无法 parse 时返回 nil, nil（视为未登录/无效）。
func (s *userServiceImpl) GetByIDStr(ctx context.Context, loginID string) (*domain.SysUser, error) {
	id, err := uuid.Parse(loginID)
	if err != nil {
		return nil, nil
	}
	return s.GetByID(ctx, id)
}

// UpdateProfile 修改用户资料。
//
// 校验逻辑与旧 controller.UpdateProfile 一致：
//   - 至少一个字段非 nil；
//   - username 非空；
//   - gender 在 [0,3]；
//   - birthdate 不在未来。
func (s *userServiceImpl) UpdateProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*UpdateProfileResult, error) {
	if input.Username == nil && input.AvatarURL == nil && input.Phone == nil && input.Gender == nil && input.Birthdate == nil {
		return nil, errAtLeastOneField
	}

	updateData := make(map[string]interface{})

	if input.Username != nil {
		username := strings.TrimSpace(*input.Username)
		if username == "" {
			return nil, errUsernameEmpty
		}
		updateData["username"] = username
	}
	if input.AvatarURL != nil {
		updateData["avatar_url"] = *input.AvatarURL
	}
	if input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		if phone == "" {
			updateData["phone"] = nil
		} else {
			updateData["phone"] = phone
		}
	}
	if input.Gender != nil {
		if *input.Gender < 0 || *input.Gender > 3 {
			return nil, errGenderRange
		}
		updateData["gender"] = *input.Gender
	}
	if input.Birthdate != nil {
		if input.Birthdate.After(time.Now()) {
			return nil, errBirthdateFuture
		}
		updateData["birthdate"] = *input.Birthdate
	}

	if err := s.repo.UpdateFields(ctx, userID, updateData); err != nil {
		return nil, err
	}

	// 重新读取最新数据返回
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return nil, err
	}

	return &UpdateProfileResult{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		AvatarURL: user.AvatarURL,
		Phone:     user.Phone,
		Gender:    user.Gender,
		Birthdate: user.Birthdate,
	}, nil
}

// Search 搜索用户。
func (s *userServiceImpl) Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*UserSearchResult, error) {
	return s.searcher.Search(ctx, keyword, size, searchAfter)
}

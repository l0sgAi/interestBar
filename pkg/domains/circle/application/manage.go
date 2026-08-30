// manage.go 圈子管理用例（角色变更/禁言/拉黑/审核/资料编辑）。
//
// 权限核心规则：只能管理角色严格低于自己的成员（owner > admin > member），
// owner 不可被禁言/拉黑/降级（转让除外）。
//
// 计数一致性：仅 status 跨 normal 边界的迁移触发 member_count 增减（拉黑 -1、
// 审核通过 +1），复用 join/leave 的 write-behind 链路（Redis Incr/Decr 真值 +
// Redpanda 事件落库 + joined ZSET 增删）。禁言仍是成员，不计数。
// P1：通知扇出（notice 域）与审计表暂未接入。
package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// maxMuteHours 禁言时长上限（30 天），防误操作造成事实上的永久封口。
const maxMuteHours = 720

// memberSearchMaxUsers 成员搜索关键词最多解析的用户数。
// circle_member 对 (circle_id, user_id) 唯一 → 过滤后结果集 ≤ 此值，
// 游标翻页在该集合内精确进行；超过则提示细化关键词。
const memberSearchMaxUsers = 100

// ===== 管理端 DTO =====

// CircleMemberItem 管理端成员列表项（含用户精简信息，由 UserFacade 组装）。
type CircleMemberItem struct {
	UserID      uuid.UUID  `json:"user_id"`
	Username    string     `json:"username,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	Role        int16      `json:"role"`
	Status      int16      `json:"status"`
	MuteEndTime *time.Time `json:"mute_end_time,omitempty"`
	JoinTime    time.Time  `json:"join_time"`
}

// CircleMemberListResult 成员列表分页结果（keyset 游标，空串 = 没有更多）。
//
// Truncated：keyword 模式解析到的候选用户数超过 memberSearchMaxUsers（100）时
// 为 true，表示结果可能不完整（超出部分按相关性丢弃）。前端应提示「细化关键词」。
// 仅 keyword 非空时可能为 true。
type CircleMemberListResult struct {
	Members   []CircleMemberItem `json:"members"`
	Size      int                `json:"size"`
	Cursor    string             `json:"cursor"`
	Truncated bool               `json:"truncated"`
}

// UpdateCircleProfileInput 编辑圈子资料入参（nil 字段 = 不更新）。
//
// 分字段权限：Name/Slug/JoinType/CategoryID 仅圈主可改；
// AvatarURL/CoverURL/Description/Rule 管理员及以上可改。
// Slug 传空串清除；CategoryID 指向 uuid.Nil 清除分类。
type UpdateCircleProfileInput struct {
	Name        *string
	Slug        *string
	AvatarURL   *string
	CoverURL    *string
	Description *string
	Rule        *string
	CategoryID  *uuid.UUID
	JoinType    *int16
}

// ===== 权限校验 =====

// requireManageRole 校验操作者是圈内管理者并返回其成员记录。
// requireRole 指定最低角色（admin/owner）；非成员视同无权限（不暴露成员身份）；
// 非正常状态（待审/禁言/拉黑/退出）的管理者暂停管理权。
func (s *circleServiceImpl) requireManageRole(ctx context.Context, circleID, operatorID uuid.UUID, requireRole int16) (*domain.CircleMember, error) {
	operator, err := s.memberRepo.GetMember(ctx, circleID, operatorID)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return nil, errNotCircleAdmin
		}
		return nil, err
	}
	if operator.Role < requireRole {
		if requireRole == domain.MemberRoleOwner {
			return nil, errNotCircleOwner
		}
		return nil, errNotCircleAdmin
	}
	if operator.Status != domain.MemberStatusNormal {
		return nil, errNotCircleAdmin
	}
	return operator, nil
}

// loadManagedTarget 加载目标成员并校验层级：只能管理角色严格低于自己的人
// （目标角色 >= 操作者 → 403，含操作者本人）。
func (s *circleServiceImpl) loadManagedTarget(ctx context.Context, operator *domain.CircleMember, circleID, targetUserID uuid.UUID) (*domain.CircleMember, error) {
	target, err := s.memberRepo.GetMember(ctx, circleID, targetUserID)
	if err != nil {
		return nil, err // ErrMemberNotFound → 404
	}
	if target.Role >= operator.Role {
		return nil, errCannotManageTarget
	}
	return target, nil
}

// requireNormalTarget 校验目标为正常状态成员（禁言/任免/转让的前置状态）。
func requireNormalTarget(target *domain.CircleMember) error {
	if target.Status != domain.MemberStatusNormal {
		return domain.ErrMemberStateConflict
	}
	return nil
}

// ===== 成员列表 =====

// validMemberFilter 校验列表过滤参数：role ∈ {-1,10,20,30}；status ∈ {-1,0..4}。
func validMemberFilter(role, status int16) bool {
	roleOK := role == -1 ||
		role == domain.MemberRoleMember || role == domain.MemberRoleAdmin || role == domain.MemberRoleOwner
	return roleOK && status >= -1 && status <= domain.MemberStatusLeft
}

// ListCircleMembers 管理端成员列表（admin+，可见全部状态含待审/拉黑）。
// 按角色（高→低）、加入时间（新→旧）keyset 分页；用户信息经 UserFacade 组装。
// keyword 非空时按用户名搜索（拼写容错，email 分词匹配亦参与召回）：先经 user 域
// 解析为候选用户集（≤memberSearchMaxUsers 个，username 权重 3 倍于 email，按相关性
// 排序；命中超上限时 Truncated=true，提示细化关键词），再以 user_id IN 过滤成员表——
// 成员表 (circle_id, user_id) 唯一，故结果集有限，游标翻页精确；翻页须带同一 keyword。
func (s *circleServiceImpl) ListCircleMembers(ctx context.Context, operatorID, circleID uuid.UUID, role, status int16, keyword, cursor string, size int) (*CircleMemberListResult, error) {
	if !validMemberFilter(role, status) {
		return nil, errInvalidMemberFilter
	}
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return nil, err
	}
	if _, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin); err != nil {
		return nil, err
	}

	// 关键词 → 候选用户集（跨域搜索在权限校验之后，避免未授权消耗 ES 查询）。
	keyword = strings.TrimSpace(keyword)
	var userIDs []uuid.UUID
	truncated := false
	if keyword != "" {
		if s.userFacade == nil {
			return nil, errUserSearchUnavailable
		}
		briefs, total, err := s.userFacade.SearchBriefs(ctx, keyword, memberSearchMaxUsers)
		if err != nil {
			logger.Log.Error("Failed to search users for member list: " + err.Error())
			return nil, errUserSearchUnavailable
		}
		// 命中总数超过解析上限：结果不完整，标记截断供前端提示。
		truncated = total > memberSearchMaxUsers
		if len(briefs) == 0 {
			// 关键词无命中用户：直接空页（不必查成员表）。
			return &CircleMemberListResult{Members: []CircleMemberItem{}, Size: size, Cursor: "", Truncated: false}, nil
		}
		userIDs = make([]uuid.UUID, 0, len(briefs))
		for _, b := range briefs {
			if id, err := uuid.Parse(b.ID); err == nil {
				userIDs = append(userIDs, id)
			}
		}
	}

	members, next, err := s.memberRepo.ListMembers(ctx, circleID, role, status, userIDs, cursor, size)
	if err != nil {
		return nil, err
	}

	// 批量组装用户精简信息（Facade 缺失或部分用户查询失败时降级为空）。
	briefs := make(map[string]UserBrief, len(members))
	if s.userFacade != nil && len(members) > 0 {
		ids := make([]string, 0, len(members))
		for i := range members {
			ids = append(ids, members[i].UserID.String())
		}
		if b, err := s.userFacade.GetBriefs(ctx, ids); err == nil {
			briefs = b
		}
	}

	items := make([]CircleMemberItem, 0, len(members))
	for i := range members {
		m := &members[i]
		item := CircleMemberItem{
			UserID:      m.UserID,
			Role:        m.Role,
			Status:      m.Status,
			MuteEndTime: m.MuteEndTime,
			JoinTime:    m.CreateTime,
		}
		if b, ok := briefs[m.UserID.String()]; ok {
			item.Username = b.Username
			item.AvatarURL = b.AvatarURL
		}
		items = append(items, item)
	}
	return &CircleMemberListResult{Members: items, Size: size, Cursor: next, Truncated: truncated}, nil
}

// ===== 角色管理 =====

// SetMemberRole 设为/取消管理员（仅圈主）。role 仅接受 10/20，转让圈主走 TransferOwner。
func (s *circleServiceImpl) SetMemberRole(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, role int16) error {
	if role != domain.MemberRoleMember && role != domain.MemberRoleAdmin {
		return errInvalidMemberRole
	}
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleOwner)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	// 前置状态机：须为正常成员且当前角色不同（已是该角色 → 409）；repo CAS 兜底并发窗口。
	if err := requireNormalTarget(target); err != nil {
		return err
	}
	if target.Role == role {
		return domain.ErrMemberStateConflict
	}
	return s.memberRepo.UpdateMemberRole(ctx, circleID, targetUserID, target.Role, role)
}

// TransferOwner 转让圈主（仅圈主；目标必须是正常状态的非圈主成员）。
func (s *circleServiceImpl) TransferOwner(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleOwner)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if err := requireNormalTarget(target); err != nil {
		return err
	}
	return s.memberRepo.TransferOwner(ctx, circleID, operatorID, targetUserID)
}

// ===== 禁言 =====

// MuteMember 禁言（admin+；时长 1-720 小时）。禁言仍是成员：member_count 不变。
func (s *circleServiceImpl) MuteMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, durationHours int) error {
	if durationHours <= 0 || durationHours > maxMuteHours {
		return errInvalidMuteDuration
	}
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if err := requireNormalTarget(target); err != nil {
		return err
	}
	muteEnd := time.Now().Add(time.Duration(durationHours) * time.Hour)
	return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusNormal, domain.MemberStatusMuted, muteEnd)
}

// UnmuteMember 解除禁言（admin+；muted → normal，清空 mute_end_time）。
func (s *circleServiceImpl) UnmuteMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusMuted {
		return domain.ErrMemberStateConflict
	}
	return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusMuted, domain.MemberStatusNormal, time.Time{})
}

// ===== 拉黑/解禁 =====

// BanMember 拉黑/踢出（admin+；normal/muted → banned）。
// 拉黑保留记录防重进（JoinCircle 对 banned 拒绝）；原状态为在圈（计数态）时
// member_count-1 + 发布事件 + joined ZSET 移除，与 LeaveCircle 同链路。
func (s *circleServiceImpl) BanMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusNormal && target.Status != domain.MemberStatusMuted {
		return domain.ErrMemberStateConflict
	}

	err = s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		target.Status, domain.MemberStatusBanned, time.Time{})
	if errors.Is(err, domain.ErrMemberStateConflict) && target.Status == domain.MemberStatusNormal {
		// 并发窗口：读到的 normal 已被并发禁言，按 muted 再 CAS 一次（两个源态都计数，副作用等价）。
		err = s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
			domain.MemberStatusMuted, domain.MemberStatusBanned, time.Time{})
	}
	if err != nil {
		return err
	}

	if err := s.decrMemberCountWithRecovery(ctx, circleID); err != nil {
		logger.Log.Error("Failed to update Redis member count: " + err.Error())
	}
	if err := s.publisher.PublishMemberCount(ctx, circleID, -1); err != nil {
		logger.Log.Error("Failed to publish ban message: " + err.Error())
	}
	if err := s.joinedCache.Remove(ctx, targetUserID, circleID); err != nil {
		logger.Log.Error("Failed to remove joined circle from cache: " + err.Error())
	}
	return nil
}

// UnbanMember 解除拉黑（admin+；banned → left）。不回圈不计数：用户需重新申请加入
// （JoinCircle 对 left → normal 已有路径）。
func (s *circleServiceImpl) UnbanMember(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusBanned {
		return domain.ErrMemberStateConflict
	}
	return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusBanned, domain.MemberStatusLeft, time.Time{})
}

// ===== 入圈审核 =====

// ReviewJoinRequest 入圈审核（admin+；pending → normal/left）。
// 通过：member_count+1 + 发布事件 + joined ZSET 写入（与 JoinCircle 同链路）。
func (s *circleServiceImpl) ReviewJoinRequest(ctx context.Context, operatorID, circleID, targetUserID uuid.UUID, approve bool) error {
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.loadManagedTarget(ctx, operator, circleID, targetUserID)
	if err != nil {
		return err
	}
	if target.Status != domain.MemberStatusPending {
		return domain.ErrMemberStateConflict
	}

	if !approve {
		return s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
			domain.MemberStatusPending, domain.MemberStatusLeft, time.Time{})
	}
	if err := s.memberRepo.UpdateMemberStatus(ctx, circleID, targetUserID,
		domain.MemberStatusPending, domain.MemberStatusNormal, time.Time{}); err != nil {
		return err
	}

	if err := s.incrMemberCountWithRecovery(ctx, circleID); err != nil {
		logger.Log.Error("Failed to update Redis member count: " + err.Error())
	}
	if err := s.publisher.PublishMemberCount(ctx, circleID, 1); err != nil {
		logger.Log.Error("Failed to publish review-approve message: " + err.Error())
	}
	// score 用申请记录的 create_time（加入时间语义与 JoinCircle 一致）。
	s.tryAddJoined(ctx, targetUserID, circleID, target.CreateTime.UnixMilli())
	return nil
}

// ===== 编辑圈子资料 =====

// UpdateCircleProfile 编辑圈子资料。分字段权限：name/slug/join_type/category_id
// 仅圈主；其余 admin+。成功后失效基础信息缓存；ES 由 CDC 追平（秒级延迟）。
func (s *circleServiceImpl) UpdateCircleProfile(ctx context.Context, operatorID, circleID uuid.UUID, input UpdateCircleProfileInput) error {
	fields := domain.CircleUpdateFields{
		Name: input.Name, Slug: input.Slug, AvatarURL: input.AvatarURL, CoverURL: input.CoverURL,
		Description: input.Description, Rule: input.Rule, CategoryID: input.CategoryID, JoinType: input.JoinType,
	}
	if fields.Name == nil && fields.Slug == nil && fields.AvatarURL == nil && fields.CoverURL == nil &&
		fields.Description == nil && fields.Rule == nil && fields.CategoryID == nil && fields.JoinType == nil {
		return errNoCircleUpdateField
	}

	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		return err
	}
	operator, err := s.requireManageRole(ctx, circleID, operatorID, domain.MemberRoleAdmin)
	if err != nil {
		return err
	}
	if operator.Role != domain.MemberRoleOwner &&
		(fields.Name != nil || fields.Slug != nil || fields.JoinType != nil || fields.CategoryID != nil) {
		return errNotCircleOwner
	}

	// 字段规整与校验：文本先 SanitizeForPg（防 PG UTF8 错误），长度按 rune 计
	// （对齐 DDL varchar 限制与创建接口的 binding 校验）。
	if fields.Name != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Name))
		if v == "" || utf8.RuneCountInString(v) > 50 {
			return errInvalidCircleProfile
		}
		fields.Name = &v
		if v != circle.Name { // 未变更不预检（否则撞自己的唯一索引）
			exists, err := s.repo.ExistsByName(ctx, v)
			if err != nil {
				return err
			}
			if exists {
				return errCircleNameExists
			}
		}
	}
	if fields.Slug != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Slug))
		if utf8.RuneCountInString(v) > 60 {
			return errInvalidCircleProfile
		}
		fields.Slug = &v
		if v != "" && v != circle.Slug {
			exists, err := s.repo.ExistsBySlug(ctx, v)
			if err != nil {
				return err
			}
			if exists {
				return errCircleSlugExists
			}
		}
	}
	if fields.Description != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Description))
		if v == "" || utf8.RuneCountInString(v) > 2000 {
			return errInvalidCircleProfile
		}
		fields.Description = &v
	}
	if fields.Rule != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.Rule))
		if utf8.RuneCountInString(v) > 2000 {
			return errInvalidCircleProfile
		}
		fields.Rule = &v
	}
	if fields.AvatarURL != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.AvatarURL))
		if utf8.RuneCountInString(v) > 500 {
			return errInvalidCircleProfile
		}
		fields.AvatarURL = &v
	}
	if fields.CoverURL != nil {
		v := utils.SanitizeForPg(strings.TrimSpace(*fields.CoverURL))
		if utf8.RuneCountInString(v) > 500 {
			return errInvalidCircleProfile
		}
		fields.CoverURL = &v
	}
	if fields.JoinType != nil {
		if *fields.JoinType < 0 || *fields.JoinType > 2 {
			return errInvalidJoinType
		}
	}

	if err := s.repo.Update(ctx, circleID, fields); err != nil {
		return err
	}
	// 失效基础信息缓存（best-effort；TTL 24h 兜底残余）。
	if err := s.baseCache.DeleteBase(ctx, circleID); err != nil {
		logger.Log.Error("Failed to invalidate circle base cache: " + err.Error())
	}
	return nil
}

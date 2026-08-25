// Package composition 的 agent_bridges.go：
// 实现 aiagent 领域的跨域端口（RoleReader / BotUserCreator），内部桥接到 user 领域。
//
// 与 user_session_store_bridge.go 同一模式：
//   - aiagent.application 只声明端口接口，不 import user 包；
//   - composition 层写桥接器把两者连起来（未来拆服务换 RPC 即可）。
package composition

import (
	"context"

	agentapp "interestBar/pkg/domains/aiagent/application"
	userapp "interestBar/pkg/domains/user/application"
	userdomain "interestBar/pkg/domains/user/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// agentRoleReader 桥接 aiagent.RoleReader -> user.UserService.GetByID（带 userinfo 缓存）。
//
// role 变更经手工改库，缓存 TTL 30 分钟内可能读到旧值（已确认接受，见设计确认项 #1）。
type agentRoleReader struct {
	delegate userapp.UserService
}

// GetUserRole 返回用户 role；用户不存在/已删/已禁用返回 false。
func (b *agentRoleReader) GetUserRole(ctx context.Context, userID uuid.UUID) (int, bool, error) {
	user, err := b.delegate.GetByID(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	if user == nil {
		return 0, false, nil
	}
	return user.Role, true, nil
}

// agentBotUserCreator 桥接 aiagent.BotUserCreator -> 直写 domains.users（role=2 机器人账号）。
//
// 过渡期直持 *gorm.DB（与 userSessionStoreBridge 一致）；
// 后续可收口为 user 领域 Service 方法。
type agentBotUserCreator struct {
	db *gorm.DB
}

// botUserRole 机器人账号 role（预留标识位，区别于 0=普通用户 / 1=管理员）。
const botUserRole = 2

// CreateBotUser 创建 role=2 的机器人系统用户，返回其 ID。
// 机器人账号不登录不发帖，仅作为 ai_agent 以该身份发评论的载体。
func (b *agentBotUserCreator) CreateBotUser(ctx context.Context, username, email string) (uuid.UUID, error) {
	u := userdomain.SysUser{
		ID:       sharedomain.NewID(),
		Username: username,
		Email:    email,
		Role:     botUserRole,
		Status:   userdomain.UserStatusActive,
		Deleted:  0,
	}
	if err := b.db.WithContext(ctx).Create(&u).Error; err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

// 编译期保证：桥接器满足 aiagent 领域端口。
var (
	_ agentapp.RoleReader     = (*agentRoleReader)(nil)
	_ agentapp.BotUserCreator = (*agentBotUserCreator)(nil)
)

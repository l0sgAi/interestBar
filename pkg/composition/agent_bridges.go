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
	commentapp "interestBar/pkg/domains/comment/application"
	postapp "interestBar/pkg/domains/post/application"
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
	_ agentapp.PostReader     = (*agentPostReader)(nil)
	_ agentapp.CommentCreator = (*agentCommentCreator)(nil)
)

// agentPostReader 桥接 aiagent.PostReader -> post.PostService.GetPostBrief。
type agentPostReader struct {
	delegate postapp.PostService
}

// GetPostBrief 返回帖子摘要。未找到返回 nil, nil。
func (b *agentPostReader) GetPostBrief(ctx context.Context, postID uuid.UUID) (*agentapp.PostBrief, error) {
	brief, err := b.delegate.GetPostBrief(ctx, postID)
	if err != nil || brief == nil {
		return nil, err
	}
	return &agentapp.PostBrief{
		ID:       brief.ID,
		Title:    brief.Title,
		Summary:  brief.Summary,
		Status:   brief.Status,
		IsLock:   brief.IsLock,
		AuthorID: brief.AuthorID,
	}, nil
}

// agentCommentCreator 桥接 aiagent.CommentCreator -> comment.CommentService.CreateComment。
//
// 机器人以 linked_user_id 身份发评论；帖子校验/清洗/计数/热度事件全部复用
// comment 域现有链路。机器人评论同样会回调 AgentReplyTrigger，由 aiagent 侧
// ExistsByLinkedUserID 防回环。
type agentCommentCreator struct {
	delegate commentapp.CommentService
}

// CreateComment 以指定用户身份创建评论，返回评论 ID。
func (b *agentCommentCreator) CreateComment(ctx context.Context, userID uuid.UUID, input agentapp.CommentCreateInput) (uuid.UUID, error) {
	return b.delegate.CreateComment(ctx, userID, commentapp.CreateCommentInput{
		PostID:    input.PostID,
		Content:   input.Content,
		RootID:    input.RootID,
		ReplyToID: input.ReplyToID,
	})
}

// commentAgentTrigger 桥接 comment.AgentReplyTrigger -> aiagent.ReplyService.OnCommentCreated。
//
// 同步回调、立即返回（ReplyService 内部 goroutine 异步执行 + recover），
// 不向评论创建链路传播任何错误。
type commentAgentTrigger struct {
	delegate agentapp.ReplyService
}

// OnCommentCreated 评论创建完成后的机器人触发入口。
func (b *commentAgentTrigger) OnCommentCreated(postID, commentID, userID uuid.UUID, rootID *uuid.UUID, content string) {
	b.delegate.OnCommentCreated(agentapp.CommentEvent{
		CommentID: commentID,
		PostID:    postID,
		UserID:    userID,
		RootID:    rootID,
		Content:   content,
	})
}

// 编译期保证：触发桥接器满足 comment 领域端口。
var _ commentapp.AgentReplyTrigger = (*commentAgentTrigger)(nil)

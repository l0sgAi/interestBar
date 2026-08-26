package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/aiagent/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"
	sharedomain "interestBar/pkg/shared/domain"
	"interestBar/pkg/util/crypto"

	"github.com/google/uuid"
)

// ===== 跨域端口（composition 桥接注入，不 import 兄弟域包）=====

// PostBrief 供机器人回复用的帖子摘要（post 域桥接提供）。
type PostBrief struct {
	ID       uuid.UUID
	Title    string
	Summary  string
	Status   int16 // 1=已发布（post.PostStatusPublished，避免跨域 import 用裸值）
	IsLock   int16
	AuthorID uuid.UUID
}

// PostReader 帖子读取端口（桥接 post 域）。未找到返回 nil, nil。
type PostReader interface {
	GetPostBrief(ctx context.Context, postID uuid.UUID) (*PostBrief, error)
}

// CommentCreateInput 机器人发评论入参（同构 comment.CreateCommentInput 的子集）。
type CommentCreateInput struct {
	PostID    uuid.UUID
	Content   string
	RootID    *uuid.UUID // 关键词触发时挂进触发评论楼层；手动触发为 nil（顶层评论）
	ReplyToID *uuid.UUID
}

// CommentCreator 评论创建端口（桥接 comment 域 CreateComment）。
type CommentCreator interface {
	CreateComment(ctx context.Context, userID uuid.UUID, input CommentCreateInput) (uuid.UUID, error)
}

// LLMRequest LLM 调用请求（application 层完成 api_key 解密，infra 层只管协议适配）。
type LLMRequest struct {
	Protocol     string // openai/anthropic（domain 白名单）
	BaseURL      string
	APIKey       string // 明文
	Model        string
	Params       map[string]interface{} // agent.LLMParamsJSON 原样
	SystemPrompt string
	UserPrompt   string
}

// LLMResult LLM 调用结果。
type LLMResult struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
}

// LLMCaller LLM 调用端口（infrastructure 用 eino 实现）。
type LLMCaller interface {
	Generate(ctx context.Context, req LLMRequest) (*LLMResult, error)
}

// CommentEvent 评论创建事件（comment 域触发钩子回调载荷）。
type CommentEvent struct {
	CommentID uuid.UUID
	PostID    uuid.UUID
	UserID    uuid.UUID
	RootID    *uuid.UUID
	Content   string
}

// ===== Service 接口 =====

// ReplyService 机器人回复执行服务（设计见 docs/agent-reply-design.md）。
type ReplyService interface {
	// OnCommentCreated 评论创建后的关键词触发入口。
	// 同步调用、立即返回（内部异步执行）；任何失败静默（仅日志表 + zap），
	// 绝不向评论创建链路返回错误。机器人自己的评论不触发（防回环）。
	OnCommentCreated(evt CommentEvent)
	// ManualReply 管理员手动触发回复（同步，仅 trigger_mode=3 的启用机器人）。
	// 返回生成的评论 ID；失败返回错误（同时已写失败日志行）。
	ManualReply(ctx context.Context, adminID, agentID, postID uuid.UUID) (uuid.UUID, error)

	// SetRoleReader 注入 user Facade（管理员校验用）。
	SetRoleReader(r RoleReader)
	// SetPostReader 注入帖子读取端口。
	SetPostReader(r PostReader)
	// SetCommentCreator 注入评论创建端口。
	SetCommentCreator(c CommentCreator)
}

// replyService 配置兜底默认值（conf <=0 时使用）。
const (
	defaultReplyTimeoutSec  = 30
	defaultMaxContentChars  = 4000
	defaultReplyConcurrency = 3
	defaultSystemPrompt     = "你是兴趣社区的智能助手，回复需友好、简洁、有帮助。"
	errMsgMaxLen            = 2048
)

type replyServiceImpl struct {
	agentRepo      domain.AgentRepository
	replyLogRepo   domain.ReplyLogRepository
	llm            LLMCaller
	roleReader     RoleReader
	postReader     PostReader
	commentCreator CommentCreator
	sem            chan struct{} // 关键词触发异步执行并发上限
}

// NewReplyService 构造 ReplyService。
//
// agentRepo/replyLogRepo/llm 为同域依赖（构造注入）；
// roleReader/postReader/commentCreator 为跨域依赖（setter 注入，composition 桥接）。
func NewReplyService(
	agentRepo domain.AgentRepository,
	replyLogRepo domain.ReplyLogRepository,
	llm LLMCaller,
) ReplyService {
	c := conf.Config.AiAgent.ReplyConcurrency
	if c <= 0 {
		c = defaultReplyConcurrency
	}
	return &replyServiceImpl{
		agentRepo:    agentRepo,
		replyLogRepo: replyLogRepo,
		llm:          llm,
		sem:          make(chan struct{}, c),
	}
}

func (s *replyServiceImpl) SetRoleReader(r RoleReader)         { s.roleReader = r }
func (s *replyServiceImpl) SetPostReader(r PostReader)         { s.postReader = r }
func (s *replyServiceImpl) SetCommentCreator(c CommentCreator) { s.commentCreator = c }

// replyTimeoutSec LLM 调用超时（配置兜底）。
func replyTimeout() time.Duration {
	sec := conf.Config.AiAgent.TimeoutSec
	if sec <= 0 {
		sec = defaultReplyTimeoutSec
	}
	return time.Duration(sec) * time.Second
}

// maxContentChars prompt 各段截断长度（配置兜底）。
func maxContentChars() int {
	n := conf.Config.AiAgent.MaxContentChars
	if n <= 0 {
		n = defaultMaxContentChars
	}
	return n
}

// OnCommentCreated 评论关键词触发（异步、静默）。
func (s *replyServiceImpl) OnCommentCreated(evt CommentEvent) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Log.Error(fmt.Sprintf("agent reply panic: %v", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), replyTimeout())
		defer cancel()

		// 防回环：机器人评论不再触发（ExistsByLinkedUserID 走 idx_ai_agent_linked_user）。
		isBot, err := s.agentRepo.ExistsByLinkedUserID(ctx, evt.UserID)
		if err != nil {
			logger.Log.Error("agent reply: check linked user failed: " + err.Error())
			return
		}
		if isBot {
			return
		}

		agents, err := s.agentRepo.ListEnabled(ctx)
		if err != nil {
			logger.Log.Error("agent reply: list enabled agents failed: " + err.Error())
			return
		}
		for i := range agents {
			agent := &agents[i]
			if domain.TriggerMode(agent.TriggerMode) != domain.TriggerModeKeyword {
				logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=mode_not_keyword", agent.ID, evt.PostID))
				continue
			}
			if !matchKeyword(evt.Content, agent.TriggerKeywords) {
				logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=keyword_miss", agent.ID, evt.PostID))
				continue
			}
			// 并发上限满则跳过本轮（尽力而为，不阻塞评论链路）。
			select {
			case s.sem <- struct{}{}:
			default:
				logger.Log.Warn(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=concurrency_full", agent.ID, evt.PostID))
				continue
			}
			// 每个命中 agent 独立 goroutine + 独立超时：sem 真正限制并发，
			// 单个慢 LLM 调用不再挤占其它机器人的时间片。
			// defer 释放信号量：executeReply panic（本 goroutine recover 兜底）
			// 也必须归还槽位，否则并发上限被永久占用。
			go func(agent *domain.AiAgent) {
				defer func() { <-s.sem }()
				defer func() {
					if r := recover(); r != nil {
						logger.Log.Error(fmt.Sprintf("agent reply panic: agent=%s post=%s: %v", agent.ID, evt.PostID, r))
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), replyTimeout())
				defer cancel()
				_, err := s.executeReply(ctx, agent, evt.PostID, &evt)
				if err != nil {
					// 前置跳过（帖子不可回/防重/限频）已在 executeReply 内打 Info，不重复；
					// 调用链路失败已写日志行，这里补一条 Error 便于告警。
					if errors.Is(err, errPostNotReplyable) || errors.Is(err, errAlreadyReplied) || errors.Is(err, errRateLimited) {
						return
					}
					logger.Log.Error(fmt.Sprintf("agent reply: agent=%s post=%s: %s",
						agent.ID, evt.PostID, err.Error()))
				}
			}(agent)
		}
	}()
}

// ManualReply 管理员手动触发（同步）。
func (s *replyServiceImpl) ManualReply(ctx context.Context, adminID, agentID, postID uuid.UUID) (uuid.UUID, error) {
	if err := s.ensureReplyAdmin(ctx, adminID); err != nil {
		return uuid.Nil, err
	}
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return uuid.Nil, err
	}
	if agent.Status != domain.AgentStatusEnabled {
		return uuid.Nil, errAgentDisabled
	}
	if domain.TriggerMode(agent.TriggerMode) != domain.TriggerModeManual {
		return uuid.Nil, errNotManualMode
	}
	commentID, err := s.executeReply(ctx, agent, postID, nil)
	if err != nil {
		return uuid.Nil, err
	}
	return *commentID, nil
}

// ensureReplyAdmin 手动触发的管理员校验（fail-closed，同 AgentService.ensureAdmin）。
func (s *replyServiceImpl) ensureReplyAdmin(ctx context.Context, adminID uuid.UUID) error {
	if s.roleReader == nil {
		return errNotAdmin
	}
	role, ok, err := s.roleReader.GetUserRole(ctx, adminID)
	if err != nil || !ok || role != roleAdmin {
		return errNotAdmin
	}
	return nil
}

// executeReply 单个机器人对单个帖子的回复执行核心（关键词/手动共用）。
//
// 返回 (成功时非nil的评论ID, error)。前置条件不满足（帖子不可回/已回复/限频）
// 返回对应哨兵错误且不写日志；调用链路失败（解密/LLM/空回复/评论落库）写失败
// 日志行后返回包装错误。
func (s *replyServiceImpl) executeReply(ctx context.Context, agent *domain.AiAgent, postID uuid.UUID, trigger *CommentEvent) (*uuid.UUID, error) {
	if s.postReader == nil || s.commentCreator == nil || s.llm == nil {
		return nil, errors.New("agent reply dependencies not configured")
	}

	// 1. 帖子门槛：已发布且未锁定。
	post, err := s.postReader.GetPostBrief(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.Status != 1 || post.IsLock == 1 {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=post_not_replyable", agent.ID, postID))
		return nil, errPostNotReplyable
	}

	// 2. 防重（含失败行，终态语义）。
	exists, err := s.replyLogRepo.ExistsByAgentAndPost(ctx, agent.ID, postID)
	if err != nil {
		return nil, err
	}
	if exists {
		logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=already_replied", agent.ID, postID))
		return nil, errAlreadyReplied
	}

	// 3. 限频：最近 1h 行数（含失败）+ 距最新一条的间隔。
	now := time.Now()
	if agent.MaxRepliesPerHour > 0 {
		count, err := s.replyLogRepo.CountSinceByAgent(ctx, agent.ID, now.Add(-time.Hour))
		if err != nil {
			return nil, err
		}
		if count >= int64(agent.MaxRepliesPerHour) {
			logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=rate_limited_hourly count=%d limit=%d",
				agent.ID, postID, count, agent.MaxRepliesPerHour))
			return nil, errRateLimited
		}
	}
	if agent.MinIntervalSec > 0 {
		last, err := s.replyLogRepo.GetLastByAgent(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		if last != nil && last.CreateTime.Add(time.Duration(agent.MinIntervalSec)*time.Second).After(now) {
			logger.Log.Info(fmt.Sprintf("agent reply skip: agent=%s post=%s reason=rate_limited_min_interval min_interval_sec=%d",
				agent.ID, postID, agent.MinIntervalSec))
			return nil, errRateLimited
		}
	}

	// 4. 调用链路（失败写日志行）。
	start := time.Now()
	var promptTokens, completionTokens int

	fail := func(errMsg string) (*uuid.UUID, error) {
		s.writeReplyLog(&domain.ReplyLog{
			AgentID: agent.ID, PostID: postID, UserID: post.AuthorID,
			Status:       domain.ReplyStatusFailed,
			ErrorMsg:     truncateErr(errMsg),
			LatencyMs:    int(time.Since(start).Milliseconds()),
			PromptTokens: promptTokens, CompletionTokens: completionTokens,
		})
		return nil, fmt.Errorf("%w: %s", errLLMCall, errMsg)
	}

	// 4.1 解密 api_key。
	apiKey := ""
	if agent.APIKeyEnc != "" {
		apiKey, err = crypto.Decrypt(conf.Config.Security.DataKey, agent.APIKeyEnc)
		if err != nil {
			return fail("decrypt api_key failed: " + err.Error())
		}
	}

	// 4.2 LLM 生成（system_prompt 空时用默认人设兜底）。
	systemPrompt := agent.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	res, err := s.llm.Generate(ctx, LLMRequest{
		Protocol:     agent.APIProtocol,
		BaseURL:      agent.BaseURL,
		APIKey:       apiKey,
		Model:        agent.Model,
		Params:       agent.LLMParams,
		SystemPrompt: systemPrompt,
		UserPrompt:   buildUserPrompt(agent, post, trigger),
	})
	if err != nil {
		return fail("llm generate failed: " + err.Error())
	}
	promptTokens, completionTokens = res.PromptTokens, res.CompletionTokens

	// 4.3 清洗输出（用户文本入库前必须过 SanitizeForPg）。
	content := utils.SanitizeForPg(strings.TrimSpace(res.Content))
	if content == "" {
		return fail("llm returned empty content")
	}

	// 4.4 评论落库：关键词触发挂进触发评论楼层，手动触发为顶层评论。
	var rootID, replyToID *uuid.UUID
	if trigger != nil {
		replyToID = &trigger.CommentID
		if trigger.RootID != nil {
			rootID = trigger.RootID
		} else {
			cid := trigger.CommentID
			rootID = &cid
		}
	}
	commentID, err := s.commentCreator.CreateComment(ctx, agent.LinkedUserID, CommentCreateInput{
		PostID: postID, Content: content, RootID: rootID, ReplyToID: replyToID,
	})
	if err != nil {
		return fail("create comment failed: " + err.Error())
	}

	// 4.5 成功日志行。
	s.writeReplyLog(&domain.ReplyLog{
		AgentID: agent.ID, PostID: postID, UserID: post.AuthorID,
		CommentID:        &commentID,
		Status:           domain.ReplyStatusOK,
		LatencyMs:        int(time.Since(start).Milliseconds()),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})
	return &commentID, nil
}

// writeReplyLog 落终态日志行（append-only）。写库失败仅告警（调用链路已尽力）。
func (s *replyServiceImpl) writeReplyLog(log *domain.ReplyLog) {
	log.ID = sharedomain.NewID()
	if err := s.replyLogRepo.Create(context.Background(), log); err != nil {
		// 并发防重撞唯一索引按已处理处理，不算错误。
		if errors.Is(err, domain.ErrReplyAlreadyExists) {
			return
		}
		logger.Log.Error("agent reply: write reply log failed: " + err.Error())
	}
}

// matchKeyword 评论内容是否命中任一触发关键词（不区分大小写子串匹配）。
func matchKeyword(content string, keywords domain.KeywordsJSON) bool {
	if len(keywords) == 0 {
		return false
	}
	lower := strings.ToLower(content)
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// buildUserPrompt 组装用户提示词：title + summary（+ 评论触发的评论原文），各段截断。
func buildUserPrompt(agent *domain.AiAgent, post *PostBrief, trigger *CommentEvent) string {
	limit := maxContentChars()
	var b strings.Builder
	b.WriteString(truncateRunes("帖子标题："+post.Title, limit))
	b.WriteString("\n")
	b.WriteString(truncateRunes("帖子摘要："+post.Summary, limit))
	b.WriteString("\n")
	if trigger != nil {
		b.WriteString(truncateRunes("用户评论："+trigger.Content, limit))
		b.WriteString("\n")
	}
	b.WriteString("请以「" + agent.Name + "」的身份对上述帖子写一条回复。")
	return b.String()
}

// truncateRunes 按字符数截断（避免截断 UTF-8 多字节字符），超长加省略号。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// truncateErr 失败原因截断（error_msg 列 varchar(2048)）。
func truncateErr(msg string) string {
	runes := []rune(msg)
	if len(runes) <= errMsgMaxLen {
		return msg
	}
	return string(runes[:errMsgMaxLen])
}

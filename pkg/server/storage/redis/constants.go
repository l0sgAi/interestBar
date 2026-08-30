package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Redis 相关键名常量定义

const (
	// RegisterCodePrefix 注册验证码缓存key前缀
	// 完整key格式: register:code:{email}
	// 存储内容：6位数字验证码，TTL 5分钟
	RegisterCodePrefix = "register:code:"

	// RegisterVerifiedPrefix 注册邮箱已验证标记key前缀
	// 完整key格式: register:verified:{email}
	// 存储内容：标记邮箱已通过验证码校验，TTL 10分钟
	RegisterVerifiedPrefix = "register:verified:"

	// RegisterRatePrefix 注册验证码发送频率限制key前缀
	// 完整key格式: register:rate:{email}
	// 存储内容：频率限制标记，TTL 60秒
	RegisterRatePrefix = "register:rate:"
)

const (
	// PwdResetCodePrefix 找回密码验证码缓存key前缀
	// 完整key格式: pwd_reset:code:{email}
	// 存储内容：6位数字验证码，TTL 5分钟
	// 与 register:code: 隔离，避免同邮箱同时进行注册和找回密码时互相覆盖
	PwdResetCodePrefix = "pwd_reset:code:"

	// PwdResetVerifiedPrefix 找回密码邮箱已验证标记key前缀
	// 完整key格式: pwd_reset:verified:{email}
	// 存储内容：标记邮箱已通过验证码校验，TTL 10分钟
	PwdResetVerifiedPrefix = "pwd_reset:verified:"

	// PwdResetRatePrefix 找回密码验证码发送频率限制key前缀
	// 完整key格式: pwd_reset:rate:{email}
	// 存储内容：频率限制标记，TTL 60秒
	PwdResetRatePrefix = "pwd_reset:rate:"
)

const (
	// CircleInfoPrefix 圈子基础信息缓存key前缀
	// 完整key格式: circle:info:{circle_id}
	// 包含字段：id, name, slug, avatar_url, cover_url, description, rule, creator_id, category_id, join_type, status, deleted, create_time, update_time
	CircleInfoPrefix = "circle:info:"

	// CircleStatsPrefix 圈子统计信息Hash key前缀
	// 完整key格式: circle:stats:{circle_id}
	// 包含字段：member_count, post_count, hot
	CircleStatsPrefix = "circle:stats:"

	// UserInfoPrefix 用户基础信息缓存key前缀
	// 完整key格式: user:info:{user_id}
	// 包含字段：id, username, email, phone, google_id, github_id, avatar_url, gender, birthdate, status, role, deleted, create_time, update_time
	UserInfoPrefix = "user:info:"

	// UserJoinedCirclesPrefix 用户已加入圈子 ZSET 缓存key前缀
	// 完整key格式: circle:joined:{user_id}
	// ZSET: member=circle_id, score=加入时间 Unix 毫秒，倒序（最近加入在前）
	// 注：前缀从 user_joined_circles: 改为 circle:joined:，避免旧 string 类型 key
	// 残留导致 ZSET 操作 WRONGTYPE（旧 key 任其 24h TTL 过期）。
	UserJoinedCirclesPrefix = "circle:joined:"
)

// CircleBaseInfo 圈子基础信息（不含统计信息）
type CircleBaseInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`
}

// CircleStatistics 圈子统计信息（用于批量更新和MQ消费等场景）
// 注意：实际读取时直接从3个独立计数器读取，不使用此结构体
type CircleStatistics struct {
	MemberCount int `json:"member_count"` // 成员数量
	PostCount   int `json:"post_count"`   // 帖子数量
	Hot         int `json:"hot"`          // 热度
}

// PostStatsPrefix 帖子统计信息Hash key前缀
// 完整key格式: post:stats:{post_id}
// 包含字段：view_count, comment_count, like_count, collect_count
const PostStatsPrefix = "post:stats:"

// PostStatistics 帖子统计信息
type PostStatistics struct {
	ViewCount    int `json:"view_count"`    // 浏览量
	CommentCount int `json:"comment_count"` // 评论数
	LikeCount    int `json:"like_count"`    // 点赞数
	CollectCount int `json:"collect_count"` // 收藏数
}

// GetUserJoinedCirclesKey 获取用户已加入圈子ID列表缓存的完整key
func GetUserJoinedCirclesKey(userID uuid.UUID) string {
	return UserJoinedCirclesPrefix + userID.String()
}

// GetUserInfoKey 获取用户基础信息缓存的完整key
func GetUserInfoKey(userID uuid.UUID) string {
	return UserInfoPrefix + userID.String()
}

// GetCircleInfoKey 获取圈子基础信息缓存的完整key
func GetCircleInfoKey(circleID uuid.UUID) string {
	return CircleInfoPrefix + circleID.String()
}

// GetCircleStatsKey 获取圈子统计信息Hash缓存的完整key
func GetCircleStatsKey(circleID uuid.UUID) string {
	return CircleStatsPrefix + circleID.String()
}

// GetPostStatsKey 获取帖子统计信息Hash缓存的完整key
func GetPostStatsKey(postID uuid.UUID) string {
	return PostStatsPrefix + postID.String()
}

// PostHotCapPrefix 帖子热度上限计数器 Hash key 前缀
// 完整key格式: post:hotcap:{post_id}
// 包含字段: comment, comment_like（已累积的 hot 贡献值，用于 clamp per-post 上限）
// 仅评论 / 评论点赞两个维度有上限；点赞 / 收藏 / 分享无上限不写此 Hash。
const PostHotCapPrefix = "post:hotcap:"

// CircleHotPrefix 圈子热度增量累加器 key 前缀
// 完整key格式: circle:hot:{circle_id}
// string(int)：累加帖子 hot 的 fan-out Δ；CircleHotSyncer 定时 GETDEL 读后清零并落库。TTL 50h。
// 与 circle:stats:{circle_id} 的 hot 字段区别：本 key 是 Δ 累加器（待落库），stats hash 是读路径热值。
const CircleHotPrefix = "circle:hot:"

// GetPostHotCapKey 获取帖子热度上限计数器 Hash 的完整 key。
func GetPostHotCapKey(postID uuid.UUID) string {
	return PostHotCapPrefix + postID.String()
}

// GetCircleHotKey 获取圈子热度增量累加器的完整 key。
func GetCircleHotKey(circleID uuid.UUID) string {
	return CircleHotPrefix + circleID.String()
}

// CFItemPrefix item-based 协同过滤「相似帖」ZSET key 前缀。
// 完整 key 格式: cf:item:{post_id}
// ZSET: member=相似 post_id(uuid 字符串), score=相似度(0..1]
// 由 ItemCFSyncer 夜级全量计算写入；TTL zset_ttl_hours(默认 48h)。
// 召回时：用户 seed 帖(点赞/收藏) → ZREVRANGE cf:item:{seed} 取 top 相似帖。
const CFItemPrefix = "cf:item:"

// GetCFItemKey 获取帖子协同过滤相似帖 ZSET 的完整 key。
func GetCFItemKey(postID uuid.UUID) string {
	return CFItemPrefix + postID.String()
}

// RecommendFeedPrefix 推荐流候选池 LIST key 前缀。
// 完整 key 格式: feed:recommend:{user_id}
// LIST: 按推荐序 RPUSH 的 post_id(uuid 字符串)；LRANGE offset 分页；TTL ttl_minutes(默认 30)。
// 池 miss/过期时由 RecommendService 触发重建（5 路召回 + 交错合并）。
const RecommendFeedPrefix = "feed:recommend:"

// RecommendFeedTokenPrefix 推荐流候选池版本 token key 前缀。
// 完整 key: feed:recommend:token:{user_id}（string），与池同 TTL。
// 客户端翻页回传 token，服务端比对：不一致 → 池已重建 → 回 offset=0（防翻页错位）。
const RecommendFeedTokenPrefix = "feed:recommend:token:"

// GetRecommendFeedKey 获取推荐流候选池 LIST 的完整 key。
func GetRecommendFeedKey(userID uuid.UUID) string {
	return RecommendFeedPrefix + userID.String()
}

// GetRecommendFeedTokenKey 获取推荐流候选池版本 token 的完整 key。
func GetRecommendFeedTokenKey(userID uuid.UUID) string {
	return RecommendFeedTokenPrefix + userID.String()
}

// UserInterestCirclesPrefix 用户「行为兴趣圈子」SET key 前缀。
// 完整 key 格式: user:interest_circles:{user_id}
// SET: 由用户点赞/收藏的 seed 帖子反查得到的 circle_id 集合（C3 行为圈子召回用）。
// miss 时从 DB 反查并落缓存；TTL interest_circles_ttl_minutes(默认 120)。读路径减去 joined circles。
const UserInterestCirclesPrefix = "user:interest_circles:"

// GetUserInterestCirclesKey 获取用户行为兴趣圈子 SET 的完整 key。
func GetUserInterestCirclesKey(userID uuid.UUID) string {
	return UserInterestCirclesPrefix + userID.String()
}

// NoticeUnreadPrefix 用户未读通知计数器 key 前缀。
// 完整 key 格式: notice:unread:{user_id}
// String 计数器: NotificationEventConsumer upsert 通知后 INCRBY；
// /notice/read 按实际已读行数 DECRBY(floor 0)；/notice/read-all SET 0；
// 读 miss 时回源 DB COUNT 回填。TTL 24h 滚动——到期 recount 校正 INCRBY/DECRBY 漂移(软信号)。
const NoticeUnreadPrefix = "notice:unread:"

// NoticeUnreadTTL 未读计数器 TTL（INCRBY/SET 时滚动续期）。
const NoticeUnreadTTL = 24 * time.Hour

// GetNoticeUnreadKey 获取用户未读通知计数器的完整 key。
func GetNoticeUnreadKey(userID uuid.UUID) string {
	return NoticeUnreadPrefix + userID.String()
}

// GetPostViewDedupeKey 获取帖子浏览去重 key
func GetPostViewDedupeKey(postID, userID uuid.UUID) string {
	return fmt.Sprintf("%s%s:%s", PostViewDedupePrefix, postID.String(), userID.String())
}

// PostViewDedupePrefix 帖子浏览去重 key 前缀
// 完整 key: post:viewdedup:{postID}:{userID}
// TTL: 5 分钟，同一用户对同一帖子在此窗口内只计一次浏览
const PostViewDedupePrefix = "post:viewdedup:"

// CommentStatsPrefix 评论统计信息Hash key前缀
// 完整key格式: comment:stats:{comment_id}
// 包含字段：like_count
const CommentStatsPrefix = "comment:stats:"

// UserCommentLikeListPrefix 用户评论点赞列表ZSET key前缀
// 完整key格式: user:like:comments:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: commentId(UUID字符串)
const UserCommentLikeListPrefix = "user:like:comments:"

// UserPostLikeListPrefix 用户帖子点赞列表ZSET key前缀
// 完整key格式: user:like:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
const UserPostLikeListPrefix = "user:like:posts:"

// GetCommentStatsKey 获取评论统计信息Hash缓存的完整key
func GetCommentStatsKey(commentID uuid.UUID) string {
	return CommentStatsPrefix + commentID.String()
}

// GetUserCommentLikeListKey 获取用户评论点赞列表ZSET的完整key
func GetUserCommentLikeListKey(userID uuid.UUID) string {
	return UserCommentLikeListPrefix + userID.String()
}

// GetUserPostLikeListKey 获取用户帖子点赞列表ZSET的完整key
func GetUserPostLikeListKey(userID uuid.UUID) string {
	return UserPostLikeListPrefix + userID.String()
}

// UserPostCollectListPrefix 用户帖子收藏列表ZSET key前缀
// 完整key格式: user:collect:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
const UserPostCollectListPrefix = "user:collect:posts:"

// GetUserPostCollectListKey 获取用户帖子收藏列表ZSET的完整key
func GetUserPostCollectListKey(userID uuid.UUID) string {
	return UserPostCollectListPrefix + userID.String()
}

// UserPostViewListPrefix 用户浏览历史列表ZSET key前缀
// 完整key格式: user:view:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
// score 倒序即「最近浏览」顺序;cap 500(超限按最低 score 淘汰);TTL 复用 postStatsTTL。
const UserPostViewListPrefix = "user:view:posts:"

// GetUserPostViewListKey 获取用户浏览历史列表ZSET的完整key
func GetUserPostViewListKey(userID uuid.UUID) string {
	return UserPostViewListPrefix + userID.String()
}

// GetRegisterCodeKey 获取注册验证码缓存的完整key
func GetRegisterCodeKey(email string) string {
	return RegisterCodePrefix + email
}

// GetRegisterVerifiedKey 获取注册邮箱已验证标记的完整key
func GetRegisterVerifiedKey(email string) string {
	return RegisterVerifiedPrefix + email
}

// GetRegisterRateKey 获取注册验证码发送频率限制的完整key
func GetRegisterRateKey(email string) string {
	return RegisterRatePrefix + email
}

// GetPwdResetCodeKey 获取找回密码验证码缓存的完整key
func GetPwdResetCodeKey(email string) string {
	return PwdResetCodePrefix + email
}

// GetPwdResetVerifiedKey 获取找回密码邮箱已验证标记的完整key
func GetPwdResetVerifiedKey(email string) string {
	return PwdResetVerifiedPrefix + email
}

// GetPwdResetRateKey 获取找回密码验证码发送频率限制的完整key
func GetPwdResetRateKey(email string) string {
	return PwdResetRatePrefix + email
}

// TrendingPrefix 热点榜单 ZSET key 前缀。
// 完整 key 格式: trending:{dimension}:{window}
//
//	dimension = post | circle | user
//	window    = 24h | 7d
//
// ZSET: member=实体 ID(uuid 字符串), score=热度（post 自身 hot / 圈子&用户为窗口内 Σhot）。
// 由 TrendingRankSyncer 周期性 ZADD 覆盖重写；不设 TTL（job 覆盖刷新），
// 不走 GETDEL（与 circle:hot Δ 累加器语义不同——那是增量累加待落库，本榜是全量重算快照）。
const TrendingPrefix = "trending:"

// TrendingMetaPrefix 热点榜单刷新时间戳 key 前缀。
// 完整 key: trending:meta:{dimension}:{window}（string, Unix 秒）。
// 读路径返回 refreshed_at，供前端显示「X 分钟前更新」与降级时标注时效。
const TrendingMetaPrefix = "trending:meta:"

// GetTrendingKey 获取热点榜单 ZSET 的完整 key。
func GetTrendingKey(dimension, window string) string {
	return TrendingPrefix + dimension + ":" + window
}

// GetTrendingMetaKey 获取热点榜单刷新时间戳的完整 key。
func GetTrendingMetaKey(dimension, window string) string {
	return TrendingMetaPrefix + dimension + ":" + window
}

// DiscoverPoolPrefix 发现页候选池 LIST key 前缀（登录用户独立池）。
// 完整 key 格式: discover:{posts|circles}:{user_id}
// LIST: 由 DiscoverPoolSyncer / 读路径 miss 随机采样后 RPUSH 写入的实体 ID(uuid 字符串)；
// LRANGE offset 翻页；TTL ttl_minutes(默认 30)。登录态为反气泡排除后的随机集。
const DiscoverPoolPrefix = "discover:"

// DiscoverAnonPrefix 发现页匿名共享池前缀。
// 完整 key: discover:anon:{posts|circles}
// 纯随机无排除，所有匿名用户共享；TTL 同登录池。匿名用户无排除集，全局随机采样。
const DiscoverAnonPrefix = "discover:anon:"

// DiscoverTokenPrefix 发现页候选池版本 token key 前缀。
// 完整 key: discover:token:{user_id|anon}（string），与池同 TTL。
// 客户端翻页回传 pool_token，服务端比对：不一致 → 池已重建 → 回 offset=0（防翻页错位）。
const DiscoverTokenPrefix = "discover:token:"

// GetDiscoverPoolKey 获取登录用户发现页候选池 LIST 的完整 key。
// section = "posts" | "circles"；userKey = user_id 字符串。
func GetDiscoverPoolKey(section, userKey string) string {
	return DiscoverPoolPrefix + section + ":" + userKey
}

// GetDiscoverAnonKey 获取匿名共享发现页候选池 LIST 的完整 key。section = "posts" | "circles"。
func GetDiscoverAnonKey(section string) string {
	return DiscoverAnonPrefix + section
}

// GetDiscoverTokenKey 获取发现页候选池版本 token 的完整 key。userKey = user_id 字符串或 "anon"。
func GetDiscoverTokenKey(userKey string) string {
	return DiscoverTokenPrefix + userKey
}

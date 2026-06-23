// Package domain 存放 auth 领域的纯领域模型：DTO、常量、接口。
//
// 依赖规则：本包不得 import 任何 gorm/redis/gin/stputil 等基础设施或框架库，
// 也不得 import 其他领域包（domains/user 等）。
//
// 跨领域约定：auth 领域需要"写入/读取用户会话信息"，但用户实体属于
// user 领域。这里定义 UserSessionStore 接口（接收最小化的 SessionUser
// 值对象），由 composition 注入一个调用 user 领域的实现。
package domain

import "context"

// SessionUser 是写入会话的最小用户视图。
//
// auth 领域登录成功后需要把用户信息塞进 sa-token session，
// 供后续 /user/get 等接口读取。但完整的 SysUser 属于 user 领域，
// 不应泄漏到 auth domain 层。故定义此值对象作为跨领域载体。
//
// 字段保持与旧 utils.SetUserToSession 写入的 SysUser 子集一致，
// 确保会话兼容（旧前端读取的 JSON 字段名不变）。
type SessionUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	GoogleID    string `json:"google_id"`
	XID         string `json:"x_id"`
	GithubID    string `json:"github_id"`
	MicrosoftID string `json:"microsoft_id"`
	AvatarURL   string `json:"avatar_url"`
	Gender      int    `json:"gender"`
}

// TokenResult 登录成功后返回给客户端的结果。
type TokenResult struct {
	User  SessionUser `json:"user"`
	Token string      `json:"token"`
}

// 用户状态常量（与 user 领域保持一致：status=1 正常）。
//
// 这里在 auth domain 里重新定义而非 import user domain，是为了保持领域间零依赖。
// 两边数值必须一致（=1），由 composition 层的 UserSessionStore 实现保证语义对齐。
const (
	UserStatusActive = 1 // 正常
)

// SaTokenSession 是 sa-token 会话的抽象接口（由 infrastructure 实现）。
//
// 抽象出来是为了让 auth domain/application 层不直接 import stputil，
// 保持框架无关。实现层（infrastructure/sa_token.go）封装 stputil 调用。
type SaTokenSession interface {
	// Login 为 loginID 在指定设备上登录，返回 token。
	Login(loginID, device string) (string, error)
	// LogoutByToken 按 token 注销登录。
	LogoutByToken(token string) error
	// Logout 注销 loginID 在指定设备上的登录。
	Logout(loginID, device string) error
	// SetSessionUser 把用户信息写入 loginID 的会话。
	SetSessionUser(loginID string, user SessionUser) error
}

// UserSessionStore 是跨领域读取"用于登录/注册的用户"的接口。
//
// 由 composition 注入实现：实现内部调用 user 领域的 UserService。
// 定义在 auth domain 层是为了让 auth service 不依赖 user 领域包。
type UserSessionStore interface {
	// GetByEmail 按邮箱查询（登录/注册时检查账号是否存在）。
	GetByEmail(email string) (*LoginUser, error)
	// Create 创建一个新用户（注册完成时），返回带 ID 的 LoginUser。
	Create(input CreateUserInput) (*LoginUser, error)
	// FindOrCreateForOAuth 按 provider ID 或 email 查找用户；
	// 不存在则创建；若按 email 匹配但缺 provider ID 则补写。
	// 返回的 LoginUser 一定含 ID。
	FindOrCreateForOAuth(lookup OAuthUserLookup) *LoginUser
}

// OAuthUserLookup 是 OAuth 登录时查找/创建用户的入参。
type OAuthUserLookup struct {
	Provider    string // google/github/azure
	LookupField string // DB 列名（google_id/github_id/microsoft_id）
	ProviderID  string
	Email       string
	Name        string
	AvatarURL   string
}

// LoginUser 是 auth 领域需要的用户视图（含密码哈希等敏感字段）。
//
// 故意与 user.domain.SysUser 分离：auth 领域只关心"登录校验所需字段"，
// 不关心资料字段（avatar/birthdate 等）。
//
// 包含所有 OAuth provider ID 字段，便于 OAuth 登录时判断"该 provider 是否已绑定"。
type LoginUser struct {
	ID          string
	Username    string
	Email       string
	Phone       string
	Pwd         string // 密码哈希（sha256）
	GoogleID    string
	XID         string
	GithubID    string
	MicrosoftID string
	AvatarURL   string
	Gender      int
	Status      int
}

// ProviderID 按 provider 名返回对应的 OAuth ID（google→GoogleID，依此类推）。
func (u *LoginUser) ProviderID(provider string) string {
	switch provider {
	case "google":
		return u.GoogleID
	case "github":
		return u.GithubID
	case "azure", "microsoft":
		return u.MicrosoftID
	}
	return ""
}

// SetProviderID 按 provider 名设置对应的 OAuth ID。
func (u *LoginUser) SetProviderID(provider, id string) {
	switch provider {
	case "google":
		u.GoogleID = id
	case "github":
		u.GithubID = id
	case "azure", "microsoft":
		u.MicrosoftID = id
	}
}

// ToSessionUser 把 LoginUser 转成可写入会话的 SessionUser。
func (u *LoginUser) ToSessionUser() SessionUser {
	return SessionUser{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		Phone:       u.Phone,
		GoogleID:    u.GoogleID,
		XID:         u.XID,
		GithubID:    u.GithubID,
		MicrosoftID: u.MicrosoftID,
		AvatarURL:   u.AvatarURL,
		Gender:      u.Gender,
	}
}

// CreateUserInput 注册新用户的输入。
type CreateUserInput struct {
	Username   string
	Email      string
	Pwd        string // 已 sha256 哈希
	AvatarURL  string
	Gender     int
	ProviderID string // OAuth 注册时填充，邮箱注册时为空
	Provider   string // google/github/microsoft/空
}

// VerificationStore 是验证码存储抽象（由 infrastructure 提供 Redis 实现）。
type VerificationStore interface {
	// SetCode 存储验证码（5 分钟过期）。
	SetCode(email, code string) error
	// GetCode 读取验证码。
	GetCode(email string) (string, error)
	// DeleteCode 删除验证码。
	DeleteCode(email string) error
	// MarkVerified 标记邮箱已校验（5 分钟过期）。
	MarkVerified(email string) error
	// IsVerified 检查邮箱是否已校验。
	IsVerified(email string) (bool, error)
	// DeleteVerified 删除已校验标记。
	DeleteVerified(email string) error
	// SetSendRateLimit 设置发送频率限制。
	SetSendRateLimit(email string) error
	// CheckSendRateLimit 检查是否处于发送频率限制中（true=受限）。
	CheckSendRateLimit(email string) (bool, error)
	// VerifyAttempt 原子地「校验验证码 + 失败计数 + 锁定 + 置 verified」。
	//
	// 一次调用完成所有副作用，杜绝并发竞态与爆破。返回 VerifyAttemptResult。
	// 错误（Redis 不可用等）返回 error，调用方应降级而非默认放行。
	VerifyAttempt(email, code string) (VerifyAttemptResult, error)
}

// VerifyAttemptStatus 原子校验状态。
type VerifyAttemptStatus string

const (
	// VerifyStatusOK 校验通过，verified 已置位。
	VerifyStatusOK VerifyAttemptStatus = "ok"
	// VerifyStatusWrong 验证码错误，仍有剩余次数。
	VerifyStatusWrong VerifyAttemptStatus = "wrong"
	// VerifyStatusLocked 失败次数耗尽，已锁定。
	VerifyStatusLocked VerifyAttemptStatus = "locked"
	// VerifyStatusExpired 验证码已过期/不存在。
	VerifyStatusExpired VerifyAttemptStatus = "expired"
)

// VerifyAttemptResult 原子校验结果。
//
//	Remaining：wrong 时为剩余尝试次数；locked 时为剩余锁定秒数；其余为 0。
type VerifyAttemptResult struct {
	Status    VerifyAttemptStatus
	Remaining int
}

// EmailSender 邮件发送抽象（由 infrastructure 提供）。
type EmailSender interface {
	// SendVerificationCode 发送验证码邮件。
	SendVerificationCode(email, code, lang string) error
}

// OAuthProvider 是 OAuth 提供方的抽象（由 infrastructure 提供具体实现）。
//
// 设计上故意把 provider ID 的读写做成"列名 + 值"的形式，而非
// ApplyProviderID(*LoginUser) 直接修改聚合。这样：
//   - domain 层不感知 SysUser/LoginUser 的具体字段；
//   - infrastructure 适配器可以包装现有的 auth.Provider 而无需重写。
type OAuthProvider interface {
	// Name 返回提供方名（google/github/azure）。
	Name() string
	// AuthCodeURL 生成跳转到 OAuth 同意页的 URL（state 用于携带 device）。
	AuthCodeURL(state string) string
	// Exchange 用 code 换 token（context 来自 HTTP 请求）。返回值不透明，
	// 只用于传给 FetchUser。
	Exchange(ctx context.Context, code string) (interface{}, error)
	// FetchUser 用 token 拉取用户信息。
	FetchUser(ctx context.Context, token interface{}) (*OAuthUserInfo, error)
	// UserLookupField 返回 DB 中存储 provider ID 的列名（google_id/github_id/microsoft_id）。
	UserLookupField() string
	// FrontendRedirectURL 返回登录成功后跳转的前端 URL。
	FrontendRedirectURL() string
}

// OAuthUserInfo 是 provider 标准化后的用户信息。
type OAuthUserInfo struct {
	ProviderID string
	Email      string
	Name       string
	AvatarURL  string
}

// OAuthProviderRegistry 是 provider 注册表的抽象。
type OAuthProviderRegistry interface {
	// Get 按 name 返回 provider，不存在返回 nil。
	Get(name string) OAuthProvider
}

// 为了避免 import "context"（保持 domain 零依赖），Exchange/FetchUser
// 使用 interface{} 而非 context.Context/oauth2.Token 类型。
// Go 标准库的 context 不算"基础设施依赖"，可以出现在 domain 层接口里
// （category 领域的 Repository 接口也是这么做的）。

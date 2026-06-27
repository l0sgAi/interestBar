// Package application 提供 auth 领域的应用服务层。
//
// 职责：登录、注册、OAuth 三大用例的编排。不关心 HTTP 层细节，
// 通过 domain 层定义的接口（SaTokenSession / UserSessionStore /
// VerificationStore / EmailSender / OAuthProviderRegistry）与基础设施解耦。
package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"interestBar/pkg/domains/auth/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/util/password"

	"go.uber.org/zap"
)

// LoginInput 邮箱密码登录入参。
type LoginInput struct {
	Email    string
	Password string
	Device   string
}

// LoginResult 登录成功结果（含 token 与会话用户视图）。
type LoginResult struct {
	User  domain.SessionUser `json:"user"`
	Token string             `json:"token"`
}

// SendCodeInput 发送验证码入参。
type SendCodeInput struct {
	Email string
	Lang  string
}

// VerifyCodeInput 校验验证码入参。
type VerifyCodeInput struct {
	Email string
	Code  string
}

// RegisterInput 完成注册入参。
type RegisterInput struct {
	Email    string
	Username string
	Password string
	Device   string
}

// OAuthLoginURLOutput OAuth 登录跳转 URL。
type OAuthLoginURLOutput struct {
	RedirectURL string
}

// AuthService 是 auth 领域的应用服务接口。
type AuthService interface {
	// Login 邮箱密码登录。
	Login(ctx context.Context, input LoginInput) (*LoginResult, error)
	// SendCode 发送注册验证码。
	SendCode(ctx context.Context, input SendCodeInput) error
	// VerifyCode 校验注册验证码。
	VerifyCode(ctx context.Context, input VerifyCodeInput) error
	// Register 完成注册（创建用户 + 自动登录）。
	Register(ctx context.Context, input RegisterInput) (*LoginResult, error)
	// OAuthLoginURL 生成 OAuth 提供方的跳转 URL（带 device 编码到 state）。
	OAuthLoginURL(ctx context.Context, provider, device string) (*OAuthLoginURLOutput, error)
	// OAuthCallback 处理 OAuth 回调（换 token + 拉用户 + 登录/注册）。
	// 返回前端跳转 URL（含 token）。
	OAuthCallback(ctx context.Context, provider, code, device string) (string, error)
	// LogoutByToken 按 token 注销登录。
	LogoutByToken(ctx context.Context, token string) error
}

type authServiceImpl struct {
	session   domain.SaTokenSession
	userStore domain.UserSessionStore
	verify    domain.VerificationStore
	email     domain.EmailSender
	oauthReg  domain.OAuthProviderRegistry
}

// NewAuthService 构造一个 AuthService。
func NewAuthService(
	session domain.SaTokenSession,
	userStore domain.UserSessionStore,
	verify domain.VerificationStore,
	email domain.EmailSender,
	oauthReg domain.OAuthProviderRegistry,
) AuthService {
	return &authServiceImpl{
		session:   session,
		userStore: userStore,
		verify:    verify,
		email:     email,
		oauthReg:  oauthReg,
	}
}

// Login 邮箱密码登录。
//
// 与旧 controller.Login 行为一致：
//  1. 按邮箱查用户；
//  2. 校验 status（禁用 → Forbidden）；
//  3. 校验密码（password.Verify 自动识别 Argon2id / 旧 SHA256）；
//  4. 若旧哈希格式 → 升级为 Argon2id（失败不阻断登录）；
//  5. 清理同设备旧 token + 登录 + 写会话。
func (s *authServiceImpl) Login(ctx context.Context, input LoginInput) (*LoginResult, error) {
	user, err := s.userStore.GetByEmail(input.Email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errInvalidCredentials
	}

	if user.Status != domain.UserStatusActive {
		return nil, errAccountDisabled
	}

	ok, needsUpgrade, verifyErr := password.Verify(input.Password, user.Pwd)
	if verifyErr != nil {
		// 哈希格式损坏：当作凭据失败处理，避免泄露内部状态
		logger.Log.Warn("password hash format invalid for user " + user.ID + ": " + verifyErr.Error())
		return nil, errInvalidCredentials
	}
	if !ok {
		return nil, errInvalidCredentials
	}

	// 透明升级：旧 SHA256 → Argon2id。失败仅记日志，不阻断登录。
	if needsUpgrade {
		if newHash, herr := password.Hash(input.Password); herr == nil {
			if uerr := s.userStore.UpdatePassword(user.ID, newHash); uerr != nil {
				logger.Log.Warn("failed to upgrade password hash for user " + user.ID + ": " + uerr.Error())
			}
		} else {
			logger.Log.Warn("failed to compute new password hash for user " + user.ID + ": " + herr.Error())
		}
	}

	device := resolveDevice(input.Device)
	userIDStr := user.ID

	// 清理同设备旧 token
	_ = s.session.Logout(userIDStr, device)

	authToken, err := s.session.Login(userIDStr, device)
	if err != nil {
		return nil, err
	}

	if err := s.session.SetSessionUser(userIDStr, user.ToSessionUser()); err != nil {
		return nil, err
	}

	return &LoginResult{
		User:  user.ToSessionUser(),
		Token: authToken,
	}, nil
}

// SendCode 发送注册验证码。
//
// 与旧 controller.SendCode 行为一致：
//  1. 校验邮箱格式；
//  2. 检查邮箱是否已注册；
//  3. 检查发送频率限制（60s）；
//  4. 生成 6 位验证码 + 写 Redis + 发邮件。
func (s *authServiceImpl) SendCode(ctx context.Context, input SendCodeInput) error {
	if _, err := mail.ParseAddress(input.Email); err != nil {
		return errInvalidEmail
	}

	existing, err := s.userStore.GetByEmail(input.Email)
	if err != nil {
		return err
	}
	if existing != nil {
		return errEmailAlreadyExists
	}

	limited, err := s.verify.CheckSendRateLimit(input.Email)
	if err != nil {
		return err
	}
	if limited {
		return errRateLimitExceeded
	}

	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())

	if err := s.verify.SetCode(input.Email, code); err != nil {
		return err
	}
	_ = s.verify.SetSendRateLimit(input.Email)

	if err := s.email.SendVerificationCode(ctx, input.Email, code, input.Lang); err != nil {
		return err
	}
	return nil
}

// VerifyCode 校验注册验证码。
//
// 与旧 controller.VerifyCode 行为一致：
//  1. 从 Redis 读验证码（不存在 → OTPExpired）；
//  2. 比对（不等 → InvalidOTP）；
//  3. 删除验证码 + 标记邮箱已校验（10 分钟有效）。
func (s *authServiceImpl) VerifyCode(ctx context.Context, input VerifyCodeInput) error {
	storedCode, err := s.verify.GetCode(input.Email)
	if err != nil {
		return errOTPExpired
	}
	if storedCode != input.Code {
		return errInvalidOTP
	}

	_ = s.verify.DeleteCode(input.Email)

	if err := s.verify.MarkVerified(input.Email); err != nil {
		return err
	}
	return nil
}

// Register 完成注册。
//
// 与旧 controller.CompleteRegistration 行为一致：
//  1. 校验 username/password 长度；
//  2. 校验邮箱是否已通过验证码校验；
//  3. 再次检查邮箱是否已注册（防并发）；
//  4. 创建用户 + 自动登录 + 写会话。
func (s *authServiceImpl) Register(ctx context.Context, input RegisterInput) (*LoginResult, error) {
	if len(input.Username) > 50 {
		return nil, errUsernameTooLong
	}
	if len(input.Password) < 6 {
		return nil, errPasswordTooShort
	}

	verified, err := s.verify.IsVerified(input.Email)
	if err != nil {
		return nil, err
	}
	if !verified {
		return nil, errOTPExpired
	}

	existing, err := s.userStore.GetByEmail(input.Email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errEmailAlreadyExists
	}

	pwdHash, err := password.Hash(input.Password)
	if err != nil {
		return nil, err
	}

	user, err := s.userStore.Create(domain.CreateUserInput{
		Username: input.Username,
		Email:    input.Email,
		Pwd:      pwdHash,
	})
	if err != nil {
		return nil, err
	}

	device := resolveDevice(input.Device)
	userIDStr := user.ID

	authToken, err := s.session.Login(userIDStr, device)
	if err != nil {
		return nil, err
	}
	if err := s.session.SetSessionUser(userIDStr, user.ToSessionUser()); err != nil {
		return nil, err
	}
	_ = s.verify.DeleteVerified(input.Email)

	return &LoginResult{
		User:  user.ToSessionUser(),
		Token: authToken,
	}, nil
}

// OAuthLoginURL 生成 OAuth 提供方的跳转 URL。
//
// state 格式与旧 controller 一致："device:<device>:<provider>-token"。
func (s *authServiceImpl) OAuthLoginURL(ctx context.Context, providerName, device string) (*OAuthLoginURLOutput, error) {
	p := s.oauthReg.Get(providerName)
	if p == nil {
		return nil, errUnknownOAuthProvider
	}
	device = resolveDevice(device)
	state := "device" + oauthStateDelimiter + device + oauthStateDelimiter + providerName + "-token"
	return &OAuthLoginURLOutput{RedirectURL: p.AuthCodeURL(state)}, nil
}

// OAuthCallback 处理 OAuth 回调。
//
// 与旧 controller.Callback 行为一致：
//  1. 用 code 换 token；
//  2. 拉取用户信息；
//  3. 按 provider ID 或 email 查库；
//  4. 不存在则创建（含头像）；
//  5. 若 provider ID 缺失则补写；
//  6. 清旧 token + 登录 + 写会话；
//  7. 返回前端跳转 URL（含 token）。
func (s *authServiceImpl) OAuthCallback(ctx context.Context, providerName, code, device string) (string, error) {
	p := s.oauthReg.Get(providerName)
	if p == nil {
		return "", errUnknownOAuthProvider
	}

	// OAuth 出站调用（换 token + 拉用户信息）单独限时，避免 provider 不可达时
	// 被 OS 层 TCP 超时拖到 ~30s；后续 DB/session 仍用原 ctx。
	oauthCtx, cancel := context.WithTimeout(ctx, oauthCallTimeout)
	defer cancel()

	token, err := p.Exchange(oauthCtx, code)
	if err != nil {
		logger.Log.Error("oauth exchange failed",
			zap.String("provider", providerName),
			zap.Error(err),
		)
		return "", classifyOAuthError(err)
	}

	userInfo, err := p.FetchUser(oauthCtx, token)
	if err != nil {
		logger.Log.Error("oauth fetch user failed",
			zap.String("provider", providerName),
			zap.Error(err),
		)
		return "", classifyOAuthError(err)
	}

	// 通过 UserSessionStore 按 (providerID OR email) 查询。
	// 注意：这里"按 provider 列名 + email"的查询由 userStore 实现内部完成，
	// auth 层不感知具体列名。
	user, err := s.findOrCreateOAuthUser(p, userInfo)
	if err != nil {
		return "", err
	}

	userIDStr := user.ID

	_ = s.session.Logout(userIDStr, device)
	authToken, err := s.session.Login(userIDStr, device)
	if err != nil {
		return "", err
	}
	if err := s.session.SetSessionUser(userIDStr, user.ToSessionUser()); err != nil {
		return "", err
	}

	frontendURL := p.FrontendRedirectURL()
	if frontendURL == "" {
		return "", errFrontendRedirectNotConfigured
	}
	return frontendURL + "?token=" + authToken, nil
}

// findOrCreateOAuthUser 按 provider ID 或 email 查用户，不存在则创建。
//
// 注意：这里通过 UserSessionStore 走的是跨领域调用（由 composition 注入）。
// provider ID 的查询/写入由 userStore 实现内部按 provider name 映射列名完成。
func (s *authServiceImpl) findOrCreateOAuthUser(p domain.OAuthProvider, info *domain.OAuthUserInfo) (*domain.LoginUser, error) {
	// 先按 email 查（userStore 的 GetByEmail 不带 provider 列），
	// 再由实现决定是否按 provider 列查询。
	//
	// 这里的语义是：把"按 (providerID OR email) 查 + 不存在则创建 + 补 provider ID"
	// 这段逻辑交给 userStore 实现（它知道 SysUser 结构）。
	// auth 层只负责调用，不关心具体表结构。
	user, err := s.userStore.FindOrCreateForOAuth(domain.OAuthUserLookup{
		Provider:    p.Name(),
		LookupField: p.UserLookupField(),
		ProviderID:  info.ProviderID,
		Email:       info.Email,
		Name:        info.Name,
		AvatarURL:   info.AvatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("find or create oauth user: %w", err)
	}
	return user, nil
}

// LogoutByToken 按 token 注销。
func (s *authServiceImpl) LogoutByToken(ctx context.Context, token string) error {
	return s.session.LogoutByToken(token)
}

// oauthCallTimeout 限制单次 OAuth 回调中出站调用（换 token + 拉用户信息）的总耗时。
const oauthCallTimeout = 15 * time.Second

// oauthStateDelimiter 用于 OAuth state 中编码 device 的分隔符（与旧 controller 一致）。
const oauthStateDelimiter = ":"

// classifyOAuthError 把 OAuth provider 的底层网络/超时错误归一为
// errOAuthProviderUnavailable，便于 handler 层返回 503（而非笼统的 500）。
// 非网络类错误（如 token 类型不符）原样返回。
func classifyOAuthError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errOAuthProviderUnavailable
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return errOAuthProviderUnavailable
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return errOAuthProviderUnavailable
	}
	return err
}

// resolveDevice 返回有效 device 值，空值/非法值默认为 web。
func resolveDevice(device string) string {
	const (
		deviceMobile = "mobile"
		deviceWeb    = "web"
	)
	if device == deviceMobile || device == deviceWeb {
		return device
	}
	return deviceWeb
}

// ParseOAuthState 从 OAuth state 中解析 device（供 handler 调用）。
//
// 与旧 controller.Callback 中的解析逻辑一致：
//
//	parts := strings.SplitN(state, ":", 3)
//	device = parts[1]
func ParseOAuthState(state string) string {
	if parts := strings.SplitN(state, oauthStateDelimiter, 3); len(parts) >= 2 {
		return resolveDevice(parts[1])
	}
	return resolveDevice("")
}

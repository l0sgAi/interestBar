package utils

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"strconv"
	"time"

	"github.com/click33/sa-token-go/core/session"
	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
)

// GetCurrentUserFromRequest 从请求中获取当前登录用户信息
// 返回 loginID (string) 和 session (interface{})
// 如果获取失败，会直接返回错误响应给客户端
func GetCurrentUserFromRequest(c *gin.Context) (string, *session.Session, bool) {
	// 从配置文件获取请求头名称
	tokenName := conf.Config.SaToken.TokenName

	// 从 Header 获取 token
	token := c.GetHeader(tokenName)
	if token == "" {
		response.Unauthorized(c, "Token not found")
		return "", nil, false
	}

	// 使用 Sa-Token-Go 获取登录用户信息
	loginID, err := stputil.GetLoginID(token)
	if err != nil {
		response.Unauthorized(c, "Invalid token")
		return "", nil, false
	}

	// 从 Session 获取用户详细信息
	session, err := stputil.GetSessionByToken(token)
	if err != nil {
		response.InternalError(c, "Failed to get session")
		return loginID, nil, false
	}

	return loginID, session, true
}

// GetLoginIDFromRequest 从请求中获取当前登录用户的 loginID
// 如果获取失败，会直接返回错误响应给客户端
func GetLoginIDFromRequest(c *gin.Context) (string, bool) {
	// 从配置文件获取请求头名称
	tokenName := conf.Config.SaToken.TokenName

	// 从 Header 获取 token
	token := c.GetHeader(tokenName)
	if token == "" {
		response.Unauthorized(c, "Token not found")
		return "", false
	}

	// 使用 Sa-Token-Go 获取登录用户信息
	loginID, err := stputil.GetLoginID(token)
	if err != nil {
		response.Unauthorized(c, "Invalid token")
		return "", false
	}

	return loginID, true
}

// GetUserIDFromRequest 从请求中获取当前登录用户的 ID (uint 类型)
// 如果获取失败，会直接返回错误响应给客户端
func GetUserIDFromRequest(c *gin.Context) (uint64, bool) {
	loginID, ok := GetLoginIDFromRequest(c)
	if !ok {
		return 0, false
	}

	// 将 login_id 转换为 uint
	userID, err := strconv.ParseUint(loginID, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return 0, false
	}

	return userID, true
}

// StoreUserSession 存储用户会话信息到 Sa-Token Session
// 在用户登录成功后调用此方法存储用户信息到 session
func StoreUserSession(userIDStr string, user model.SysUser) error {
	session, err := stputil.GetSession(userIDStr)
	if err != nil {
		return err
	}

	// 存储用户基本信息
	session.Set("user_id", user.ID)
	session.Set("email", user.Email)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("avatar_url", user.AvatarURL)
	session.Set("login_time", time.Now().Format(time.RFC3339))

	// 存储 OAuth 相关信息
	if user.GoogleID != "" {
		session.Set("google_id", user.GoogleID)
	}
	if user.GithubID != "" {
		session.Set("github_id", user.GithubID)
	}

	return nil
}

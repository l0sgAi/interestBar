package utils

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"strconv"
	"time"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
)

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

// SessionKeyForUser session中存储用户信息的key
const SessionKeyForUser = "user_info"

// SetUserToSession 将用户信息存储到当前session
func SetUserToSession(loginID string, user *model.SysUser) error {
	session, err := stputil.GetSession(loginID)
	if err != nil {
		return err
	}
	return session.Set(SessionKeyForUser, user)
}

// GetUserFromSession 从当前请求的session中获取用户信息
func GetUserFromSession(c *gin.Context) (*model.SysUser, bool) {
	loginID, ok := GetLoginIDFromRequest(c)
	if !ok {
		return nil, false
	}

	// 从 session 获取用户信息
	session, err := stputil.GetSession(loginID)
	if err != nil {
		response.InternalError(c, "Failed to get session")
		return nil, false
	}

	// 尝试直接从 session 获取用户信息
	userData, exists := session.Get(SessionKeyForUser)
	if !exists || userData == nil {
		response.InternalError(c, "User info not found in session")
		return nil, false
	}

	// userData 可能是 map[string]interface{} 类型，需要转换为 SysUser
	// 先尝试类型断言
	var user *model.SysUser

	switch v := userData.(type) {
	case *model.SysUser:
		user = v
	case map[string]interface{}:
		// 从 map 中手动提取字段
		user = &model.SysUser{}
		if id, ok := v["id"].(float64); ok {
			user.ID = int64(id)
		}
		if username, ok := v["username"].(string); ok {
			user.Username = username
		}
		if email, ok := v["email"].(string); ok {
			user.Email = email
		}
		if phone, ok := v["phone"].(string); ok {
			user.Phone = phone
		}
		if googleID, ok := v["google_id"].(string); ok {
			user.GoogleID = googleID
		}
		if githubID, ok := v["github_id"].(string); ok {
			user.GithubID = githubID
		}
		if avatarURL, ok := v["avatar_url"].(string); ok {
			user.AvatarURL = avatarURL
		}
		if gender, ok := v["gender"].(float64); ok {
			user.Gender = int(gender)
		}
		if status, ok := v["status"].(float64); ok {
			user.Status = int(status)
		}
		if role, ok := v["role"].(float64); ok {
			user.Role = int(role)
		}
		// 处理时间字段
		if createTime, ok := v["create_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createTime); err == nil {
				user.CreateTime = t
			}
		}
		if updateTime, ok := v["update_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, updateTime); err == nil {
				user.UpdateTime = t
			}
		}
	default:
		response.InternalError(c, "Invalid user data type in session")
		return nil, false
	}

	if user == nil {
		response.InternalError(c, "Failed to parse user data from session")
		return nil, false
	}

	return user, true
}

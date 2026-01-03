package controller

import (
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/cache/redis"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/util"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserController defines the interface for user operations.
type UserController struct{}

func NewUserController() *UserController {
	return &UserController{}
}

func (ctrl *UserController) GetUser(c *gin.Context) {
	// TODO: Implement GetUser logic
	response.Success(c, gin.H{"message": "get user stub"})
}

// Logout handles user logout
func (ctrl *UserController) Logout(c *gin.Context) {
	// Get token from context (set by AuthMiddleware)
	token, exists := c.Get("token")
	if !exists {
		response.BadRequest(c, response.MsgTokenRequired)
		return
	}

	tokenStr, ok := token.(string)
	if !ok {
		response.InternalError(c)
		return
	}

	// Delete token from Redis
	err := redis.DeleteToken(tokenStr)
	if err != nil {
		response.InternalError(c, "Failed to logout")
		return
	}

	// Get user ID and delete session
	userID, exists := c.Get("user_id")
	if exists {
		userIDUint, ok := userID.(uint)
		if ok {
			redis.DeleteUserSession(userIDUint)
		}
	}

	response.SuccessWithMessage(c, "Logout successful", nil)
}

// GetCurrentUser returns the current authenticated user info
func (ctrl *UserController) GetCurrentUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		response.Unauthorized(c)
		return
	}

	email, _ := c.Get("email")
	role, _ := c.Get("role")

	response.Success(c, gin.H{
		"user_id": userID,
		"email":   email,
		"role":    role,
	})
}

// GoogleLogin redirects the user to the Google OAuth login page
func (ctrl *UserController) GoogleLogin(c *gin.Context) {
	config := auth.GetGoogleOAuthConfig()
	// In production, generating a random state is recommended to prevent CSRF
	url := config.AuthCodeURL("state-token")
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// GoogleCallback handles the callback from Google
func (ctrl *UserController) GoogleCallback(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		response.BadRequest(c, "Code not found")
		return
	}

	config := auth.GetGoogleOAuthConfig()
	token, err := config.Exchange(c, code)
	if err != nil {
		response.InternalError(c, "Failed to exchange token: "+err.Error())
		return
	}

	googleUser, err := auth.GetGoogleUser(token)
	if err != nil {
		response.InternalError(c, "Failed to get user info")
		return
	}

	var user model.SysUser
	// Check if user exists by Google ID or Email
	result := pgsql.DB.Where("(google_id = ? OR email = ?) AND deleted = ?", googleUser.ID, googleUser.Email, 0).First(&user)

	if result.Error != nil {
		// 用户不存在，执行自动注册
		if result.Error == gorm.ErrRecordNotFound {
			// 处理用户名为空的情况，如果 Google 没返回名字，就截取邮箱前缀
			username := googleUser.Name
			if username == "" {
				username = strings.Split(googleUser.Email, "@")[0]
			}

			newUser := model.SysUser{
				Username:   username,
				Email:      googleUser.Email,
				GoogleID:   googleUser.ID,
				AvatarURL:  googleUser.Picture, // 假设 GoogleUser 有 Picture 字段
				Role:       0,                  // 默认普通用户
				Status:     1,                  // 默认状态正常
				Deleted:    0,
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
			}

			// 插入数据库
			if createErr := pgsql.DB.Create(&newUser).Error; createErr != nil {
				response.InternalError(c, "Failed to create user account: "+createErr.Error())
				return
			}

			// 将创建好的用户赋值给 user 变量，供后续生成 token 使用
			user = newUser
		} else {
			// 真正的数据库错误
			response.InternalError(c, response.MsgDatabaseError)
			return
		}
	}

	// User found
	// If GoogleID is missing (matched by email), update it
	if user.GoogleID == "" {
		user.GoogleID = googleUser.ID
		pgsql.DB.Save(&user)
	}

	// Delete all old tokens for this user (session invalidation)
	// This ensures only one active session per user
	// Comment this out if you want to allow multiple concurrent sessions
	err = redis.DeleteAllUserTokens(user.ID)
	if err != nil {
		// Log error but don't fail the login
		// logger.Log.Warn("Failed to delete old tokens: " + err.Error())
	}

	// Generate Login Token
	authToken, err := util.GenerateToken(user.ID, user.Email, user.Role)
	if err != nil {
		response.InternalError(c, "Failed to generate auth token")
		return
	}

	// Store token in Redis with 3 days expiration
	err = redis.SetToken(authToken, user.ID, util.TokenExpiration)
	if err != nil {
		response.InternalError(c, "Failed to store token")
		return
	}

	// Store user session in Redis
	sessionData := map[string]interface{}{
		"user_id":    user.ID,
		"email":      user.Email,
		"username":   user.Username,
		"role":       user.Role,
		"login_time": time.Now().Format(time.RFC3339),
	}
	err = redis.SetUserSession(user.ID, sessionData, util.TokenExpiration)
	if err != nil {
		// Log error but don't fail the login
		// logger.Log.Warn("Failed to store user session: " + err.Error())
	}

	response.Success(c, gin.H{
		"token":  authToken,
		"expire": util.TokenExpiration.String(),
	})
}

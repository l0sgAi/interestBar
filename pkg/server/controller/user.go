package controller

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/utils"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserController defines the interface for user operations.
type UserController struct{}

func NewUserController() *UserController {
	return &UserController{}
}

func (ctrl *UserController) GetUser(c *gin.Context) {
	// 从 sa-token session 获取用户信息
	user, ok := utils.GetUserFromSession(c)
	if !ok {
		return
	}

	// 返回用户信息
	response.Success(c, user)
}

// Logout handles user logout
func (ctrl *UserController) Logout(c *gin.Context) {
	// 使用工具类获取用户ID
	loginID, exists := utils.GetUserIDFromRequest(c)
	if !exists {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// Sa-Token登出
	err := stputil.Logout(loginID)
	if err != nil {
		response.InternalError(c, "Failed to logout")
		return
	}

	response.SuccessWithMessage(c, "Logout successful", nil)
}

// GetCurrentUser returns the current authenticated user info
func (ctrl *UserController) GetCurrentUser(c *gin.Context) {
	// 从 sa-token session 获取用户信息
	user, ok := utils.GetUserFromSession(c)
	if !ok {
		return
	}

	// 返回用户信息
	response.Success(c, user)
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
		logger.Log.Error("Failed to exchange token: " + err.Error())
		response.InternalError(c, "Failed to exchange token")
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
				AvatarURL:  googleUser.Picture,
				Role:       0,
				Status:     1,
				Deleted:    0,
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
			}

			// 插入数据库
			if createErr := pgsql.DB.Create(&newUser).Error; createErr != nil {
				logger.Log.Error("Failed to create user account: " + err.Error())
				response.InternalError(c, "Failed to create user account")
				return
			}

			user = newUser
		} else {
			response.InternalError(c, response.MsgDatabaseError)
			return
		}
	}

	// If GoogleID is missing (matched by email), update it
	if user.GoogleID == "" {
		user.GoogleID = googleUser.ID
		pgsql.DB.Save(&user)
	}

	// 使用 Sa-Token-Go 登录 (使用用户 ID 作为 loginId)
	userIDStr := strconv.FormatUint(uint64(user.ID), 10)

	// 登录前先注销该用户的所有旧 session,避免 token 积累
	// TODO: 如果需要清理token，可以考虑先登出，但是这样就无法多端登录了
	// stputil.Logout(userIDStr)

	authToken, err := stputil.Login(userIDStr)
	if err != nil {
		response.InternalError(c, "Failed to login")
		return
	}

	// 将用户信息存储到 sa-token session
	if err := utils.SetUserToSession(userIDStr, &user); err != nil {
		response.InternalError(c, "Failed to store user info in session")
		return
	}

	// 重定向到前端页面,并将 token 作为参数传递
	frontendURL := conf.Config.Oauth.Google.FrontendRedirectURL
	if frontendURL == "" {
		response.InternalError(c, "Frontend redirect URL not configured")
		return
	}

	redirectURL := frontendURL + "?token=" + authToken
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// GithubLogin redirects the user to the GitHub OAuth login page
func (ctrl *UserController) GithubLogin(c *gin.Context) {
	config := auth.GetGithubOAuthConfig()
	// In production, generating a random state is recommended to prevent CSRF
	url := config.AuthCodeURL("state-token")
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// GithubCallback handles the callback from GitHub
func (ctrl *UserController) GithubCallback(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		response.BadRequest(c, "Code not found")
		return
	}

	config := auth.GetGithubOAuthConfig()
	token, err := config.Exchange(c, code)
	if err != nil {
		logger.Log.Error("Failed to exchange token: " + err.Error())
		response.InternalError(c, "Failed to exchange token")
		return
	}

	githubUser, err := auth.GetGithubUser(token)
	if err != nil {
		response.InternalError(c, "Failed to get user info")
		return
	}

	var user model.SysUser
	githubID := strconv.FormatInt(githubUser.ID, 10)
	// Check if user exists by GitHub ID or Email
	result := pgsql.DB.Where("(github_id = ? OR email = ?) AND deleted = ?", githubID, githubUser.Email, 0).First(&user)

	if result.Error != nil {
		// 用户不存在，执行自动注册
		if result.Error == gorm.ErrRecordNotFound {
			// 处理用户名为空的情况，如果 GitHub 没返回名字，就使用 login
			username := githubUser.Name
			if username == "" {
				username = githubUser.Login
			}

			newUser := model.SysUser{
				Username:   username,
				Email:      githubUser.Email,
				GithubID:   githubID,
				AvatarURL:  githubUser.AvatarURL,
				Role:       0,
				Status:     1,
				Deleted:    0,
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
			}

			// 插入数据库
			if createErr := pgsql.DB.Create(&newUser).Error; createErr != nil {
				logger.Log.Error("Failed to create user account: " + err.Error())
				response.InternalError(c, "Failed to create user account")
				return
			}

			user = newUser
		} else {
			response.InternalError(c, response.MsgDatabaseError)
			return
		}
	}

	// If GithubID is missing (matched by email), update it
	if user.GithubID == "" {
		user.GithubID = githubID
		pgsql.DB.Save(&user)
	}

	// 使用 Sa-Token-Go 登录 (使用用户 ID 作为 loginId)
	userIDStr := strconv.FormatUint(uint64(user.ID), 10)

	// 登录前先注销该用户的所有旧 session,避免 token 积累
	// TODO: 如果需要清理token，可以考虑先登出，但是这样就无法多端登录了
	// stputil.Logout(userIDStr)

	authToken, err := stputil.Login(userIDStr)
	if err != nil {
		response.InternalError(c, "Failed to login")
		return
	}

	// 将用户信息存储到 sa-token session
	if err := utils.SetUserToSession(userIDStr, &user); err != nil {
		response.InternalError(c, "Failed to store user info in session")
		return
	}

	// 重定向到前端页面,并将 token 作为参数传递
	frontendURL := conf.Config.Oauth.Github.FrontendRedirectURL
	if frontendURL == "" {
		response.InternalError(c, "Frontend redirect URL not configured")
		return
	}

	redirectURL := frontendURL + "?token=" + authToken
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// UpdateProfileRequest 修改用户信息的请求结构
type UpdateProfileRequest struct {
	Username  *string    `json:"username" binding:"omitempty,min=1,max=50"`
	AvatarURL *string    `json:"avatar_url" binding:"omitempty,url"`
	Phone     *string    `json:"phone" binding:"omitempty"`
	Gender    *int       `json:"gender" binding:"omitempty,min=0,max=2"`
	Birthdate *time.Time `json:"birthdate" binding:"omitempty"`
}

// UpdateProfile 修改用户自身信息（用户名、头像、手机号、性别、生日）
func (ctrl *UserController) UpdateProfile(c *gin.Context) {
	// 使用工具类获取用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 至少需要修改一个字段
	if req.Username == nil && req.AvatarURL == nil && req.Phone == nil && req.Gender == nil && req.Birthdate == nil {
		response.BadRequest(c, "At least one field must be provided")
		return
	}

	// 从数据库获取当前用户信息
	var user model.SysUser
	if err := pgsql.DB.Where("id = ? AND deleted = ?", userID, 0).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "User not found")
		} else {
			response.InternalError(c, "Failed to get user info")
		}
		return
	}

	// 更新字段
	updateData := make(map[string]interface{})

	if req.Username != nil {
		username := strings.TrimSpace(*req.Username)
		if username == "" {
			response.BadRequest(c, "Username cannot be empty")
			return
		}
		updateData["username"] = username
	}

	if req.AvatarURL != nil {
		updateData["avatar_url"] = *req.AvatarURL
	}

	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		// 如果传入了空字符串，设置为 NULL（删除手机号）
		if phone == "" {
			updateData["phone"] = nil
		} else {
			// 可以在这里添加手机号格式验证
			updateData["phone"] = phone
		}
	}

	if req.Gender != nil {
		// 验证性别值：0=未知, 1=男, 2=女
		if *req.Gender < 0 || *req.Gender > 2 {
			response.BadRequest(c, "Gender must be 0 (unknown), 1 (male), or 2 (female)")
			return
		}
		updateData["gender"] = *req.Gender
	}

	if req.Birthdate != nil {
		// 验证生日不能是未来时间
		if req.Birthdate.After(time.Now()) {
			response.BadRequest(c, "Birthdate cannot be in the future")
			return
		}
		updateData["birthdate"] = *req.Birthdate
	}

	// 更新数据库
	if err := pgsql.DB.Model(&user).Updates(updateData).Error; err != nil {
		response.InternalError(c, "Failed to update user info")
		return
	}

	// 刷新数据库中的用户数据
	if err := pgsql.DB.Where("id = ? AND deleted = ?", userID, 0).First(&user).Error; err != nil {
		response.InternalError(c, "Failed to refresh user info")
		return
	}

	// 同步更新 session 中的用户信息（保持会话一致性）
	loginID := strconv.FormatUint(uint64(userID), 10)
	if err := utils.SetUserToSession(loginID, &user); err != nil {
		// session 更新失败不影响主流程
		// 可以考虑添加日志记录
	}

	response.SuccessWithMessage(c, "Profile updated successfully", gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"email":      user.Email,
		"avatar_url": user.AvatarURL,
		"phone":      user.Phone,
		"gender":     user.Gender,
		"birthdate":  user.Birthdate,
	})
}

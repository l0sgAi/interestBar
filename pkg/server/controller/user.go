package controller

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
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
	// 从上下文中获取 login_id（由Sa-Token中间件设置）
	loginID, exists := c.Get("login_id")
	if !exists {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// 将 login_id 转换为 uint
	userID, err := strconv.ParseUint(loginID.(string), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}

	// 使用带缓存的 GetUserByID 获取用户信息
	user, err := model.GetUserByID(pgsql.DB, uint(userID))
	if err != nil {
		response.InternalError(c, "Failed to get user info")
		return
	}

	if user == nil {
		response.NotFound(c, "User not found")
		return
	}

	// 返回用户信息
	response.Success(c, user)
}

// Logout handles user logout
func (ctrl *UserController) Logout(c *gin.Context) {
	loginID, exists := c.Get("login_id")
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
	loginID, exists := c.Get("login_id")
	if !exists {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// 从 Session 获取用户详细信息
	session, err := stputil.GetSession(loginID)
	if err != nil {
		response.InternalError(c, "Failed to get session")
		return
	}

	response.Success(c, gin.H{
		"user_id":  loginID,
		"email":    session.GetString("email"),
		"username": session.GetString("username"),
		"role":     session.GetInt("role"),
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
				AvatarURL:  googleUser.Picture,
				Role:       0,
				Status:     1,
				Deleted:    0,
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
			}

			// 插入数据库
			if createErr := pgsql.DB.Create(&newUser).Error; createErr != nil {
				response.InternalError(c, "Failed to create user account: "+createErr.Error())
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

	// 存储用户会话信息到 Sa-Token Session
	session, err := stputil.GetSession(userIDStr)
	if err == nil {
		session.Set("user_id", user.ID)
		session.Set("email", user.Email)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("google_id", user.GoogleID)
		session.Set("avatar_url", user.AvatarURL)
		session.Set("login_time", time.Now().Format(time.RFC3339))
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
		response.InternalError(c, "Failed to exchange token: "+err.Error())
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
				response.InternalError(c, "Failed to create user account: "+createErr.Error())
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

	// 存储用户会话信息到 Sa-Token Session
	session, err := stputil.GetSession(userIDStr)
	if err == nil {
		session.Set("user_id", user.ID)
		session.Set("email", user.Email)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("github_id", user.GithubID)
		session.Set("avatar_url", user.AvatarURL)
		session.Set("login_time", time.Now().Format(time.RFC3339))
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

package controller

import (
	"encoding/json"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/utils"
	"strings"
	"time"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	token := c.GetHeader(conf.Config.SaToken.TokenName)
	if token == "" {
		response.Unauthorized(c, "Token not found")
		return
	}

	err := stputil.LogoutByToken(token)
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

// UpdateProfileRequest 修改用户信息的请求结构
type UpdateProfileRequest struct {
	Username  *string    `json:"username" binding:"omitempty,min=1,max=50"`
	AvatarURL *string    `json:"avatar_url" binding:"omitempty,url"`
	Phone     *string    `json:"phone" binding:"omitempty"`
	Gender    *int       `json:"gender" binding:"omitempty,min=0,max=3"`
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
		// 验证性别值：0=未知, 1=男, 2=女 3 = 其它
		if *req.Gender < 0 || *req.Gender > 3 {
			response.BadRequest(c, "Gender must be 0 (unknown), 1 (male), or 2 (female) 3 (others)")
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
	loginID := userID.String()
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

// SearchUsersRequest 搜索用户的请求结构
type SearchUsersRequest struct {
	Keyword     string `form:"keyword"`      // 搜索关键字
	Size        int    `form:"size"`         // 每页数量，默认20
	SearchAfter string `form:"search_after"` // 上一页返回的search_after值（JSON字符串）
}

// UserListItemVO 用户列表项VO
type UserListItemVO struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	AvatarURL  string `json:"avatar_url"`
	Gender     int8   `json:"gender"`
	Role       int8   `json:"role"`
	CreateTime string `json:"create_time"`
}

// SearchUsers 搜索用户
// GET /user/search
func (ctrl *UserController) SearchUsers(c *gin.Context) {
	// 解析请求参数
	var req SearchUsersRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 设置默认每页数量
	size := req.Size
	if size <= 0 || size > 100 {
		size = 20
	}

	// 解析 search_after 参数
	var searchAfter []interface{}
	if req.SearchAfter != "" {
		if err := json.Unmarshal([]byte(req.SearchAfter), &searchAfter); err != nil {
			response.BadRequest(c, "Invalid search_after parameter")
			return
		}
	}

	// 调用 Elasticsearch 搜索
	result, err := elasticsearch.SearchUsers(req.Keyword, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search users: " + err.Error())
		response.InternalError(c, "Failed to search users")
		return
	}

	// 构建用户列表VO
	users := make([]UserListItemVO, 0, len(result.Users))
	for _, doc := range result.Users {
		user := UserListItemVO{
			ID:         doc.ID,
			Username:   doc.Username,
			Email:      doc.Email,
			AvatarURL:  doc.AvatarURL,
			Gender:     doc.Gender,
			Role:       doc.Role,
			CreateTime: doc.CreateTime,
		}
		users = append(users, user)
	}

	// 将 search_after 转换为 JSON 字符串返回
	var searchAfterJSON string
	if result.SearchAfter != nil {
		if bytes, err := json.Marshal(result.SearchAfter); err == nil {
			searchAfterJSON = string(bytes)
		}
	}

	// 构建响应数据
	responseData := map[string]interface{}{
		"total":        result.Total,
		"size":         result.Size,
		"search_after": searchAfterJSON,
		"data":         users,
	}

	response.Success(c, responseData)
}

// GetUserDetail 获取用户详情
// GET /user/detail/:id
func (ctrl *UserController) GetUserDetail(c *gin.Context) {
	// 获取用户ID参数 (UUIDv7)
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid user id")
		return
	}

	// 定义缓存key
	redisKey := redispkg.GetUserInfoKey(userID)

	// 1. 先尝试从缓存获取用户信息
	var user model.SysUser
	err = redispkg.GetJSONCompressed(redisKey, &user)

	if err == nil {
		// 缓存命中，验证用户状态
		if user.Status != 1 || user.Deleted != 0 {
			response.NotFound(c, "No such user")
			return
		}
		// 直接返回缓存数据
		response.Success(c, user)
		return
	}

	// 2. 缓存未命中，从数据库查询
	dbUser, err := model.GetUserByID(pgsql.DB, userID)
	if err != nil {
		response.InternalError(c, "Failed to get user info")
		return
	}

	if dbUser == nil {
		response.NotFound(c, "No such user")
		return
	}

	// 3. 验证用户状态（只返回status=1且deleted=0的用户）
	if dbUser.Status != 1 || dbUser.Deleted != 0 {
		response.NotFound(c, "No such user")
		return
	}

	// 4. 写入缓存（30分钟过期）
	if err := redispkg.SetJSONCompressed(redisKey, dbUser, 30*time.Minute); err != nil {
		// 缓存写入失败记录日志，但不影响主流程
		logger.Log.Error("Failed to cache user info: " + err.Error())
	}

	// 5. 返回用户信息
	response.Success(c, dbUser)
}

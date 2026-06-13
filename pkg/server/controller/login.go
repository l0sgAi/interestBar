package controller

import (
	"crypto/sha256"
	"fmt"
	"net/http"

	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/utils"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
)

type LoginController struct{}

func NewLoginController() *LoginController {
	return &LoginController{}
}

type loginReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Device   string `json:"device"` // mobile / web，可选，默认 web
}

// Login 邮箱密码登录
func (ctrl *LoginController) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, response.MsgMissingParameter)
		return
	}

	user, err := model.GetUserByEmail(pgsql.DB, req.Email)
	if err != nil {
		response.InternalError(c, response.MsgDatabaseError)
		return
	}
	if user == nil {
		response.Unauthorized(c, response.MsgInvalidCredentials)
		return
	}

	if user.Status != 1 {
		response.Forbidden(c, response.MsgAccountDisabled)
		return
	}

	pwdHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.Password)))
	if user.Pwd != pwdHash {
		response.Unauthorized(c, response.MsgInvalidCredentials)
		return
	}

	userIDStr := user.ID.String()

	// 清理同设备的旧 token（直接删除 key，避免 KICK_OUT 残留）
	_ = stputil.Logout(userIDStr, utils.ResolveDevice(req.Device))

	authToken, err := stputil.Login(userIDStr, utils.ResolveDevice(req.Device))
	if err != nil {
		response.InternalError(c, "Failed to login")
		return
	}

	if err := utils.SetUserToSession(userIDStr, user); err != nil {
		response.InternalError(c, "Failed to store user info in session")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    response.CodeSuccess,
		"message": "Login successful",
		"data": gin.H{
			"user":  user,
			"token": authToken,
		},
	})
}

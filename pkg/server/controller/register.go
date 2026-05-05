package controller

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"net/http"
	"net/mail"
	"strconv"
	"time"

	emailutil "interestBar/pkg/util/email"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/utils"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
)

type RegisterController struct{}

func NewRegisterController() *RegisterController {
	return &RegisterController{}
}

type sendCodeReq struct {
	Email string `json:"email" binding:"required"`
	Lang  string `json:"lang"`
}

// SendCode 发送注册验证码
func (ctrl *RegisterController) SendCode(c *gin.Context) {
	var req sendCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, response.MsgMissingParameter)
		return
	}

	if _, err := mail.ParseAddress(req.Email); err != nil {
		response.BadRequest(c, response.MsgInvalidEmail)
		return
	}

	existingUser, err := model.GetUserByEmail(pgsql.DB, req.Email)
	if err != nil {
		response.InternalError(c, response.MsgDatabaseError)
		return
	}
	if existingUser != nil {
		response.Conflict(c, response.MsgEmailAlreadyExists)
		return
	}

	limited, err := redis.CheckSendRateLimit(req.Email)
	if err != nil {
		response.InternalError(c, response.MsgRedisError)
		return
	}
	if limited {
		response.TooManyRequests(c, response.MsgRateLimitExceeded)
		return
	}

	code := fmt.Sprintf("%06d", rand.Intn(1000000))

	if err := redis.SetVerificationCode(req.Email, code); err != nil {
		response.InternalError(c, response.MsgRedisError)
		return
	}

	_ = redis.SetSendRateLimit(req.Email)

	client := emailutil.GetClient()
	if client == nil {
		response.InternalError(c, "Email service unavailable")
		return
	}

	if err := client.SendVerificationCode(c.Request.Context(), req.Email, code, req.Lang); err != nil {
		response.InternalError(c, "Failed to send verification email")
		return
	}

	response.Success(c, nil)
}

type verifyCodeReq struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// VerifyCode 校验注册验证码
func (ctrl *RegisterController) VerifyCode(c *gin.Context) {
	var req verifyCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, response.MsgMissingParameter)
		return
	}

	storedCode, err := redis.GetVerificationCode(req.Email)
	if err != nil {
		// redis.Nil means key does not exist (expired)
		response.ErrorWithMessage(c, response.CodeBadRequest, response.MsgOTPExpired)
		return
	}

	if storedCode != req.Code {
		response.BadRequest(c, response.MsgInvalidOTP)
		return
	}

	_ = redis.DeleteVerificationCode(req.Email)

	if err := redis.SetEmailVerified(req.Email); err != nil {
		response.InternalError(c, response.MsgRedisError)
		return
	}

	response.Success(c, nil)
}

type completeReq struct {
	Email    string `json:"email" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// CompleteRegistration 完成注册
func (ctrl *RegisterController) CompleteRegistration(c *gin.Context) {
	var req completeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, response.MsgMissingParameter)
		return
	}

	if len(req.Username) > 50 {
		response.BadRequest(c, "Username must be at most 50 characters")
		return
	}
	if len(req.Password) < 6 {
		response.BadRequest(c, "Password must be at least 6 characters")
		return
	}

	verified, err := redis.IsEmailVerified(req.Email)
	if err != nil {
		response.InternalError(c, response.MsgRedisError)
		return
	}
	if !verified {
		response.BadRequest(c, response.MsgOTPExpired)
		return
	}

	existingUser, err := model.GetUserByEmail(pgsql.DB, req.Email)
	if err != nil {
		response.InternalError(c, response.MsgDatabaseError)
		return
	}
	if existingUser != nil {
		response.Conflict(c, response.MsgEmailAlreadyExists)
		return
	}

	pwdHash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.Password)))

	user := model.SysUser{
		Username:   req.Username,
		Email:      req.Email,
		Pwd:        pwdHash,
		Role:       0,
		Status:     1,
		Deleted:    0,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	if err := pgsql.DB.Create(&user).Error; err != nil {
		response.InternalError(c, "Failed to create user account")
		return
	}

	userIDStr := strconv.FormatUint(uint64(user.ID), 10)

	authToken, err := stputil.Login(userIDStr)
	if err != nil {
		response.InternalError(c, "Failed to login")
		return
	}

	if err := utils.SetUserToSession(userIDStr, &user); err != nil {
		response.InternalError(c, "Failed to store user info in session")
		return
	}

	_ = redis.DeleteEmailVerified(req.Email)

	c.JSON(http.StatusOK, gin.H{
		"code":    response.CodeSuccess,
		"message": "Registration successful",
		"data": gin.H{
			"user":  user,
			"token": authToken,
		},
	})
}

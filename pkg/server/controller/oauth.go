package controller

import (
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/utils"
	"net/http"
	"strings"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// oauthStateDelimiter 用于 OAuth state 中编码 device 的分隔符
const oauthStateDelimiter = ":"

type OAuthController struct{}

func NewOAuthController() *OAuthController {
	return &OAuthController{}
}

// Login returns a gin.HandlerFunc that redirects to the provider's OAuth consent page.
func (ctrl *OAuthController) Login(providerName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := auth.GetProvider(providerName)
		if p == nil {
			response.BadRequest(c, "Unknown OAuth provider")
			return
		}

		device := utils.ResolveDevice(c.Query("device"))
		// state 格式: device:<device>:<provider>-token，用于回调时提取 device
		state := "device" + oauthStateDelimiter + device + oauthStateDelimiter + providerName + "-token"
		url := p.OAuthConfig().AuthCodeURL(state)
		c.Redirect(http.StatusTemporaryRedirect, url)
	}
}

// Callback returns a gin.HandlerFunc that handles the OAuth callback for any provider.
func (ctrl *OAuthController) Callback(providerName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := auth.GetProvider(providerName)
		if p == nil {
			response.BadRequest(c, "Unknown OAuth provider")
			return
		}

		// 从 state 中提取 device
		state := c.Query("state")
		device := utils.DeviceWeb
		if parts := strings.SplitN(state, oauthStateDelimiter, 3); len(parts) >= 2 {
			device = utils.ResolveDevice(parts[1])
		}

		code := c.Query("code")
		if code == "" {
			response.BadRequest(c, "Code not found")
			return
		}

		token, err := p.OAuthConfig().Exchange(c, code)
		if err != nil {
			logger.Log.Error("Failed to exchange token: " + err.Error())
			response.InternalError(c, "Failed to exchange token")
			return
		}

		userInfo, err := p.FetchUser(c, token)
		if err != nil {
			response.InternalError(c, "Failed to get user info")
			return
		}

		lookupField := p.UserLookupField()
		var user model.SysUser
		result := pgsql.DB.Where(
			"("+lookupField+" = ? OR email = ?) AND deleted = ?",
			userInfo.ProviderID, userInfo.Email, 0,
		).First(&user)

		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				username := userInfo.Name
				if username == "" {
					username = strings.Split(userInfo.Email, "@")[0]
				}

				newUser := model.SysUser{
					Username:  username,
					Email:     userInfo.Email,
					AvatarURL: userInfo.AvatarURL,
					Role:      0,
					Status:    1,
					Deleted:   0,
				}
				p.ApplyProviderID(&newUser, userInfo.ProviderID)

				if createErr := pgsql.DB.Create(&newUser).Error; createErr != nil {
					logger.Log.Error("Failed to create user account: " + createErr.Error())
					response.InternalError(c, "Failed to create user account")
					return
				}
				user = newUser
			} else {
				response.InternalError(c, response.MsgDatabaseError)
				return
			}
		}

		// Update provider ID if user was matched by email but lacks the provider link
		if p.GetProviderID(&user) == "" {
			p.ApplyProviderID(&user, userInfo.ProviderID)
			pgsql.DB.Save(&user)
		}

		userIDStr := user.ID.String()

		// 清理同设备的旧 token（直接删除 key，避免 KICK_OUT 残留）
		_ = stputil.Logout(userIDStr, device)

		authToken, err := stputil.Login(userIDStr, device)
		if err != nil {
			response.InternalError(c, "Failed to login")
			return
		}

		if err := utils.SetUserToSession(userIDStr, &user); err != nil {
			response.InternalError(c, "Failed to store user info in session")
			return
		}

		frontendURL := p.FrontendRedirectURL()
		if frontendURL == "" {
			response.InternalError(c, "Frontend redirect URL not configured")
			return
		}

		redirectURL := frontendURL + "?token=" + authToken
		c.Redirect(http.StatusTemporaryRedirect, redirectURL)
	}
}

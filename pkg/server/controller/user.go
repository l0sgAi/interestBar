package controller

import (
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/util"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserController defines the interface for user operations.
type UserController struct{}

func NewUserController() *UserController {
	return &UserController{}
}

func (ctrl *UserController) Login(c *gin.Context) {
	// TODO: Implement Login logic
	c.JSON(200, gin.H{"message": "login stub"})
}

func (ctrl *UserController) Register(c *gin.Context) {
	// TODO: Implement Register logic
	c.JSON(200, gin.H{"message": "register stub"})
}

func (ctrl *UserController) GetUser(c *gin.Context) {
	// TODO: Implement GetUser logic
	c.JSON(200, gin.H{"message": "get user stub"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Code not found"})
		return
	}

	config := auth.GetGoogleOAuthConfig()
	token, err := config.Exchange(c, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to exchange token"})
		return
	}

	googleUser, err := auth.GetGoogleUser(token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user info"})
		return
	}

	var user model.SysUser
	// Check if user exists by Google ID or Email
	result := pgsql.DB.Where("google_id = ? OR email = ?", googleUser.ID, googleUser.Email).First(&user)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// User not found, issue binding token for registration
			bindingToken, err := util.GenerateBindingToken("google", googleUser.ID, googleUser.Email)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate binding token"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code":          201, // 201 indicates "Created" or in this case "Need Creation" flow
				"message":       "User not found, please register",
				"binding_token": bindingToken,
				"email":         googleUser.Email,
				"google_id":     googleUser.ID,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// User found
	// If GoogleID is missing (matched by email), update it
	if user.GoogleID == "" {
		user.GoogleID = googleUser.ID
		pgsql.DB.Save(&user)
	}

	// Generate Login Token
	authToken, err := util.GenerateToken(user.ID, user.Email, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate auth token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  200,
		"token": authToken,
		"user":  user,
	})
}

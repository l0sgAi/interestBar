package auth

import (
	"context"
	"encoding/json"
	"fmt" // Assuming config access path, may need adjustment
	"net/http"

	"interestBar/pkg/conf"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleUser represents the structure of user data returned by Google
type GoogleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Locale        string `json:"locale"`
}

// GetGoogleOAuthConfig creates the oauth2.Config for Google
func GetGoogleOAuthConfig() *oauth2.Config {
	// 检查点：确保 Config 已经被初始化了
	if conf.Config == nil {
		panic("配置尚未初始化，请先调用 InitConfig")
	}

	return &oauth2.Config{
		// 直接从结构体中取值，既安全又有代码提示
		RedirectURL:  conf.Config.Oauth.Google.RedirectURL,
		ClientID:     conf.Config.Oauth.Google.ClientID,
		ClientSecret: conf.Config.Oauth.Google.ClientSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

// GetGoogleUser fetches user info from Google using the access token
func GetGoogleUser(token *oauth2.Token) (*GoogleUser, error) {
	client := GetGoogleOAuthConfig().Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status code %d", resp.StatusCode)
	}

	var user GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %v", err)
	}

	return &user, nil
}

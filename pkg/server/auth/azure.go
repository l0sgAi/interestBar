package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"interestBar/pkg/conf"
	"interestBar/pkg/server/model"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

// AzureUser represents the structure of user data returned by Microsoft Graph API
type AzureUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

// GetAzureOAuthConfig creates the oauth2.Config for Microsoft Azure AD
func GetAzureOAuthConfig() *oauth2.Config {
	if conf.Config == nil {
		panic("配置尚未初始化，请先调用 InitConfig")
	}

	return &oauth2.Config{
		RedirectURL:  conf.Config.Oauth.Microsoft.RedirectURL,
		ClientID:     conf.Config.Oauth.Microsoft.ClientID,
		ClientSecret: conf.Config.Oauth.Microsoft.ClientSecret,
		Scopes: []string{
			"User.Read",
		},
		Endpoint: microsoft.AzureADEndpoint("common"),
	}
}

// GetAzureUser fetches user info from Microsoft Graph API using the access token
func GetAzureUser(token *oauth2.Token) (*AzureUser, error) {
	client := GetAzureOAuthConfig().Client(context.Background(), token)
	resp, err := client.Get("https://graph.microsoft.com/v1.0/me")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get user info: status code %d", resp.StatusCode)
	}

	var user AzureUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %v", err)
	}

	return &user, nil
}

// AzureProvider implements Provider for Microsoft Azure AD OAuth2.
type AzureProvider struct{}

func (p *AzureProvider) Name() string { return "azure" }

func (p *AzureProvider) OAuthConfig() *oauth2.Config {
	return GetAzureOAuthConfig()
}

func (p *AzureProvider) FetchUser(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error) {
	au, err := GetAzureUser(token)
	if err != nil {
		return nil, err
	}
	// Microsoft Graph: Mail may be empty, fall back to UserPrincipalName
	email := au.Mail
	if email == "" {
		email = au.UserPrincipalName
	}
	return &OAuthUserInfo{
		ProviderID: au.ID,
		Email:      email,
		Name:       au.DisplayName,
	}, nil
}

func (p *AzureProvider) UserLookupField() string { return "microsoft_id" }

func (p *AzureProvider) ApplyProviderID(user *model.SysUser, providerID string) {
	user.MicrosoftID = providerID
}

func (p *AzureProvider) GetProviderID(user *model.SysUser) string {
	return user.MicrosoftID
}

func (p *AzureProvider) FrontendRedirectURL() string {
	return conf.Config.Oauth.Microsoft.FrontendRedirectURL
}

package auth

import (
	"context"

	"interestBar/pkg/server/model"

	"golang.org/x/oauth2"
)

// OAuthUserInfo is a provider-normalized representation of the external user.
type OAuthUserInfo struct {
	ProviderID string
	Email      string
	Name       string
	AvatarURL  string
}

// Provider defines the contract for an OAuth2 provider.
type Provider interface {
	Name() string
	OAuthConfig() *oauth2.Config
	FetchUser(ctx context.Context, token *oauth2.Token) (*OAuthUserInfo, error)
	UserLookupField() string
	ApplyProviderID(user *model.SysUser, providerID string)
	GetProviderID(user *model.SysUser) string
	FrontendRedirectURL() string
}

var providers = map[string]Provider{
	"google": &GoogleProvider{},
	"github": &GithubProvider{},
	"azure":  &AzureProvider{},
}

// GetProvider returns the Provider for the given name, or nil.
func GetProvider(name string) Provider {
	return providers[name]
}

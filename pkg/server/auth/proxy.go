package auth

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

// oauthClient 是 OAuth 出站（换 token / 拉用户信息）使用的 HTTP 客户端。
// 根据 conf.Oauth.ProxyURL 配置代理；为空则直连。懒构建一次后复用。
//
// 注意：客户端在首次使用时按当时的配置构建；若运行期通过 viper 热更新修改
// proxy_url，需重启进程才能生效。
var (
	oauthClientOnce sync.Once
	oauthClient     *http.Client
)

// oauthHTTPClient 返回（懒构建的）OAuth 出站 HTTP 客户端。
// ProxyURL 非空时启用代理；解析失败则回落直连并记录错误日志。
func oauthHTTPClient() *http.Client {
	oauthClientOnce.Do(func() {
		oauthClient = buildOAuthHTTPClient()
	})
	return oauthClient
}

func buildOAuthHTTPClient() *http.Client {
	// 基于 http.DefaultTransport 克隆，保留默认连接池/Keep-Alive 等设置。
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if conf.Config != nil && conf.Config.Oauth.ProxyURL != "" {
		proxyURL, err := url.Parse(conf.Config.Oauth.ProxyURL)
		if err != nil {
			logger.Log.Error("invalid oauth proxy_url, falling back to direct connection",
				zap.String("proxy_url", conf.Config.Oauth.ProxyURL),
				zap.Error(err),
			)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			logger.Log.Info("oauth outbound proxy enabled",
				zap.String("proxy", conf.Config.Oauth.ProxyURL),
			)
		}
	}

	// 兜底整体超时；正常路径的出站耗时由 service 层 oauthCallTimeout（15s）约束。
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// WithHTTPClient 把代理感知的 OAuth HTTP 客户端注入 context。
//
// oauth2.Config.Exchange 与 oauth2.Config.Client 均会通过 oauth2.HTTPClient
// 这个 context key 读取客户端，从而让换 token 与拉用户信息都走代理
// （并受调用方传入 ctx 的超时约束）。供 infrastructure 层适配器调用。
func WithHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, oauthHTTPClient())
}

package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"go.uber.org/zap"
)

var (
	EmailClient *Client
	once        sync.Once
)

// mailtrapRequest 对应 Mailtrap 发送 API 的请求体
type mailtrapRequest struct {
	From     mailtrapAddress   `json:"from"`
	To       []mailtrapAddress `json:"to"`
	Subject  string            `json:"subject"`
	Text     string            `json:"text,omitempty"`
	HTML     string            `json:"html,omitempty"`
	Category string            `json:"category,omitempty"`
}

type mailtrapAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// SendOptions 邮件发送的可选参数，零值字段使用配置默认值
type SendOptions struct {
	FromEmail string // 覆盖默认发件人邮箱
	FromName  string // 覆盖默认发件人名称
	Category  string // Mailtrap 分类标签
	TextBody  string // 纯文本正文
	HTMLBody  string // HTML 正文（TextBody 和 HTMLBody 至少填一个）
}

// Client Mailtrap 邮件客户端
type Client struct {
	apiToken    string
	apiURL      string
	senderEmail string
	senderName  string
	httpClient  *http.Client
}

// InitEmail 初始化全局 Mailtrap 邮件客户端
func InitEmail() error {
	var initErr error
	once.Do(func() {
		cfg := conf.Config.Mailtrap

		if cfg.APIToken == "" {
			initErr = fmt.Errorf("mailtrap configuration is incomplete: api_token is required")
			return
		}

		apiURL := cfg.APIURL
		if apiURL == "" {
			apiURL = "https://send.api.mailtrap.io/api/send"
		}

		EmailClient = &Client{
			apiToken:    cfg.APIToken,
			apiURL:      apiURL,
			senderEmail: cfg.SenderEmail,
			senderName:  cfg.SenderName,
			httpClient: &http.Client{
				Timeout: 10 * time.Second,
			},
		}

		logger.Log.Info("Mailtrap email client initialized successfully",
			zap.String("sender_email", cfg.SenderEmail),
			zap.String("api_url", apiURL),
		)
	})
	return initErr
}

// GetClient 获取已初始化的邮件客户端
func GetClient() *Client {
	if EmailClient == nil {
		logger.Log.Error("Mailtrap email client is not initialized, call InitEmail first")
		return nil
	}
	return EmailClient
}

// Send 通过 Mailtrap API 发送邮件
func (c *Client) Send(ctx context.Context, toEmail, toName, subject string, opts SendOptions) error {
	if toEmail == "" {
		return fmt.Errorf("recipient email is required")
	}
	if opts.TextBody == "" && opts.HTMLBody == "" {
		return fmt.Errorf("at least one of text body or html body is required")
	}

	fromEmail := opts.FromEmail
	if fromEmail == "" {
		fromEmail = c.senderEmail
	}
	fromName := opts.FromName
	if fromName == "" {
		fromName = c.senderName
	}

	reqBody := mailtrapRequest{
		From:     mailtrapAddress{Email: fromEmail, Name: fromName},
		To:       []mailtrapAddress{{Email: toEmail, Name: toName}},
		Subject:  subject,
		Text:     opts.TextBody,
		HTML:     opts.HTMLBody,
		Category: opts.Category,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal email request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Mailtrap response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailtrap API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	logger.Log.Info("email sent successfully",
		zap.String("to", toEmail),
		zap.String("subject", subject),
	)
	return nil
}

// SendVerificationCode 发送验证码邮件
func (c *Client) SendVerificationCode(ctx context.Context, toEmail, code string) error {
	subject := "Your Verification Code"
	textBody := fmt.Sprintf("Your verification code is: %s\n\nThis code will expire in a few minutes. If you did not request this code, please ignore this email.", code)

	return c.Send(ctx, toEmail, "", subject, SendOptions{
		TextBody: textBody,
		Category: "verification",
	})
}

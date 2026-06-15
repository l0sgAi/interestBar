package application

import "errors"

// 领域层可识别错误。handler 层据此映射 HTTP 响应。
var (
	errInvalidCredentials          = errors.New("invalid credentials")
	errAccountDisabled             = errors.New("account has been disabled")
	errInvalidEmail                = errors.New("invalid email format")
	errEmailAlreadyExists          = errors.New("email already registered")
	errRateLimitExceeded           = errors.New("rate limit exceeded")
	errOTPExpired                  = errors.New("verification code expired")
	errInvalidOTP                  = errors.New("invalid verification code")
	errUsernameTooLong             = errors.New("username must be at most 50 characters")
	errPasswordTooShort            = errors.New("password must be at least 6 characters")
	errUnknownOAuthProvider        = errors.New("unknown OAuth provider")
	errFrontendRedirectNotConfigured = errors.New("frontend redirect URL not configured")
)

// 错误判断函数（供 handler 层使用）。
func IsInvalidCredentialsErr(err error) bool          { return errors.Is(err, errInvalidCredentials) }
func IsAccountDisabledErr(err error) bool              { return errors.Is(err, errAccountDisabled) }
func IsInvalidEmailErr(err error) bool                 { return errors.Is(err, errInvalidEmail) }
func IsEmailAlreadyExistsErr(err error) bool           { return errors.Is(err, errEmailAlreadyExists) }
func IsRateLimitExceededErr(err error) bool            { return errors.Is(err, errRateLimitExceeded) }
func IsOTPExpiredErr(err error) bool                   { return errors.Is(err, errOTPExpired) }
func IsInvalidOTPErr(err error) bool                   { return errors.Is(err, errInvalidOTP) }
func IsUsernameTooLongErr(err error) bool              { return errors.Is(err, errUsernameTooLong) }
func IsPasswordTooShortErr(err error) bool             { return errors.Is(err, errPasswordTooShort) }
func IsUnknownOAuthProviderErr(err error) bool         { return errors.Is(err, errUnknownOAuthProvider) }
func IsFrontendRedirectNotConfiguredErr(err error) bool {
	return errors.Is(err, errFrontendRedirectNotConfigured)
}

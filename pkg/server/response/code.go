package response

import (
	"net/http"
)

// ResponseCode defines the response code type
type ResponseCode int

const (
	// Success codes
	CodeSuccess ResponseCode = 200 + iota

	// Client error codes (4xx)
	CodeBadRequest
	CodeUnauthorized
	CodeForbidden
	CodeNotFound
	CodeMethodNotAllowed
	CodeRequestTimeout
	CodeConflict
	CodeTooManyRequests
	CodeValidationError

	// Server error codes (5xx)
	CodeInternalError
	CodeNotImplemented
	CodeServiceUnavailable
)

// Response represents a standard API response structure
type Response struct {
	Code    ResponseCode `json:"code"`
	Message string       `json:"message"`
	Data    interface{}  `json:"data,omitempty"`
}

// PaginationResponse represents a paginated response
type PaginationResponse struct {
	Code    ResponseCode `json:"code"`
	Message string       `json:"message"`
	Data    interface{}  `json:"data,omitempty"`
	Total   int64        `json:"total,omitempty"`
	Page    int          `json:"page,omitempty"`
	PerPage int          `json:"per_page,omitempty"`
}

// Predefined error messages
const (
	// Success messages
	MsgSuccess = "Success"
	MsgCreated = "Created successfully"
	MsgUpdated = "Updated successfully"
	MsgDeleted = "Deleted successfully"

	// Error messages
	MsgBadRequest          = "Bad request"
	MsgUnauthorized        = "Authentication required"
	MsgInvalidToken        = "Invalid or expired token"
	MsgForbidden           = "Access forbidden"
	MsgNotFound            = "Resource not found"
	MsgMethodNotAllowed    = "Method not allowed"
	MsgRequestTimeout      = "Request timeout"
	MsgConflict            = "Resource conflict"
	MsgTooManyRequests     = "Too many requests"
	MsgValidationError     = "Validation failed"
	MsgInternalError       = "Internal server error"
	MsgNotImplemented      = "Feature not implemented"
	MsgServiceUnavailable  = "Service unavailable"
	MsgDatabaseError       = "Database error"
	MsgRedisError          = "Cache error"
	MsgInvalidCredentials  = "Invalid credentials"
	MsgUserNotFound        = "User not found"
	MsgUserExists          = "User already exists"
	MsgInvalidEmail        = "Invalid email format"
	MsgInvalidPassword     = "Invalid password"
	MsgEmailAlreadyExists  = "Email already registered"
	MsgTokenRequired       = "Token is required"
	MsgInvalidOTP          = "Invalid verification code"
	MsgOTPExpired          = "Verification code expired"
	MsgAccountDisabled     = "Account has been disabled"
	MsgInsufficientBalance = "Insufficient balance"
	MsgInvalidParameter    = "Invalid parameter"
	MsgMissingParameter    = "Missing required parameter"
	MsgInvalidFormat       = "Invalid format"
	MsgRateLimitExceeded   = "Rate limit exceeded"
	MsgCSRFTokenRequired   = "CSRF token is required"
	MsgInvalidCSRFToken    = "Invalid CSRF token"
	MsgOriginNotAllowed    = "Origin not allowed"
	MsgSessionExpired      = "Session has expired"
	MsgLoginRequired       = "Please login first"
	MsgPermissionDenied    = "Permission denied"
	MsgOperationFailed     = "Operation failed"
)

// CodeMessage maps response codes to their default messages
var CodeMessage = map[ResponseCode]string{
	CodeSuccess:            MsgSuccess,
	CodeBadRequest:         MsgBadRequest,
	CodeUnauthorized:       MsgUnauthorized,
	CodeForbidden:          MsgForbidden,
	CodeNotFound:           MsgNotFound,
	CodeMethodNotAllowed:   MsgMethodNotAllowed,
	CodeRequestTimeout:     MsgRequestTimeout,
	CodeConflict:           MsgConflict,
	CodeTooManyRequests:    MsgTooManyRequests,
	CodeValidationError:    MsgValidationError,
	CodeInternalError:      MsgInternalError,
	CodeNotImplemented:     MsgNotImplemented,
	CodeServiceUnavailable: MsgServiceUnavailable,
}

// HTTPStatusMap maps response codes to HTTP status codes
var HTTPStatusMap = map[ResponseCode]int{
	CodeSuccess:            http.StatusOK,
	CodeBadRequest:         http.StatusBadRequest,
	CodeUnauthorized:       http.StatusUnauthorized,
	CodeForbidden:          http.StatusForbidden,
	CodeNotFound:           http.StatusNotFound,
	CodeMethodNotAllowed:   http.StatusMethodNotAllowed,
	CodeRequestTimeout:     http.StatusRequestTimeout,
	CodeConflict:           http.StatusConflict,
	CodeTooManyRequests:    http.StatusTooManyRequests,
	CodeValidationError:    http.StatusBadRequest,
	CodeInternalError:      http.StatusInternalServerError,
	CodeNotImplemented:     http.StatusNotImplemented,
	CodeServiceUnavailable: http.StatusServiceUnavailable,
}

// GetHTTPStatus returns the HTTP status code for a response code
func GetHTTPStatus(code ResponseCode) int {
	if status, ok := HTTPStatusMap[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// GetMessage returns the default message for a response code
func GetMessage(code ResponseCode) string {
	if msg, ok := CodeMessage[code]; ok {
		return msg
	}
	return "Unknown error"
}

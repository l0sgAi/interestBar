package response

import (
	"github.com/gin-gonic/gin"
)

// Success sends a successful response with data
func Success(c *gin.Context, data interface{}) {
	c.JSON(GetHTTPStatus(CodeSuccess), Response{
		Code:    CodeSuccess,
		Message: GetMessage(CodeSuccess),
		Data:    data,
	})
}

// SuccessWithMessage sends a successful response with custom message
func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(GetHTTPStatus(CodeSuccess), Response{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
	})
}

// Created sends a 201 Created response
func Created(c *gin.Context, data interface{}) {
	c.JSON(GetHTTPStatus(CodeSuccess), Response{
		Code:    CodeSuccess,
		Message: MsgCreated,
		Data:    data,
	})
}

// Error sends an error response
func Error(c *gin.Context, code ResponseCode) {
	c.JSON(GetHTTPStatus(code), Response{
		Code:    code,
		Message: GetMessage(code),
	})
}

// ErrorWithMessage sends an error response with custom message
func ErrorWithMessage(c *gin.Context, code ResponseCode, message string) {
	c.JSON(GetHTTPStatus(code), Response{
		Code:    code,
		Message: message,
	})
}

// ErrorWithData sends an error response with additional data
func ErrorWithData(c *gin.Context, code ResponseCode, message string, data interface{}) {
	c.JSON(GetHTTPStatus(code), Response{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

// BadRequest sends a 400 Bad Request response
func BadRequest(c *gin.Context, message ...string) {
	msg := MsgBadRequest
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeBadRequest, msg)
}

// Unauthorized sends a 401 Unauthorized response
func Unauthorized(c *gin.Context, message ...string) {
	msg := MsgUnauthorized
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeUnauthorized, msg)
}

// Forbidden sends a 403 Forbidden response
func Forbidden(c *gin.Context, message ...string) {
	msg := MsgForbidden
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeForbidden, msg)
}

// NotFound sends a 404 Not Found response
func NotFound(c *gin.Context, message ...string) {
	msg := MsgNotFound
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeNotFound, msg)
}

// ValidationError sends a validation error response
func ValidationError(c *gin.Context, message ...string) {
	msg := MsgValidationError
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeValidationError, msg)
}

// InternalError sends a 500 Internal Server Error response
func InternalError(c *gin.Context, message ...string) {
	msg := MsgInternalError
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeInternalError, msg)
}

// Conflict sends a 409 Conflict response
func Conflict(c *gin.Context, message ...string) {
	msg := MsgConflict
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeConflict, msg)
}

// TooManyRequests sends a 429 Too Many Requests response
func TooManyRequests(c *gin.Context, message ...string) {
	msg := MsgTooManyRequests
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorWithMessage(c, CodeTooManyRequests, msg)
}

// Pagination sends a paginated response
func Pagination(c *gin.Context, data interface{}, total int64, page int, perPage int) {
	c.JSON(GetHTTPStatus(CodeSuccess), PaginationResponse{
		Code:    CodeSuccess,
		Message: GetMessage(CodeSuccess),
		Data:    data,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// PaginationWithMessage sends a paginated response with custom message
func PaginationWithMessage(c *gin.Context, message string, data interface{}, total int64, page int, perPage int) {
	c.JSON(GetHTTPStatus(CodeSuccess), PaginationResponse{
		Code:    CodeSuccess,
		Message: message,
		Data:    data,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

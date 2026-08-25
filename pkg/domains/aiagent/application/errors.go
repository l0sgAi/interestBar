package application

import (
	"errors"
)

// 领域层可识别错误（handler 经谓词映射为 HTTP 响应）。
var (
	errNotAdmin         = errors.New("admin role required")
	errAgentNotFound    = errors.New("agent not found")
	errAgentNameExists  = errors.New("agent name already exists")
	errInvalidName      = errors.New("agent name must be 1-50 chars")
	errInvalidProtocol  = errors.New("api_protocol must be openai/anthropic")
	errInvalidModel     = errors.New("model must be 1-100 chars")
	errInvalidTrigger   = errors.New("trigger_mode must be 1/2/3; mode 2 requires keywords")
	errInvalidLLMParams = errors.New("llm_params has invalid key/value")
	errInvalidRateLimit = errors.New("rate limit values must be >= 0")
	errInvalidStatus    = errors.New("status must be 0 or 1")
	errAPIKeyNotSet     = errors.New("data key not configured (security.data_key)")
	errNoFieldsToUpdate = errors.New("at least one field to update")
)

// 回复执行链路错误（reply_service）。
var (
	errAgentDisabled    = errors.New("agent is disabled")
	errNotManualMode    = errors.New("agent is not in manual trigger mode")
	errPostNotReplyable = errors.New("post not replyable (not published or locked)")
	errAlreadyReplied   = errors.New("agent already replied to this post")
	errRateLimited      = errors.New("agent reply rate limited")
	errLLMCall          = errors.New("llm call failed")
)

// 错误判断谓词（供 handler 层使用）。
func IsNotAdminErr(err error) bool         { return errors.Is(err, errNotAdmin) }
func IsAgentNotFoundErr(err error) bool    { return errors.Is(err, errAgentNotFound) }
func IsAgentNameExistsErr(err error) bool  { return errors.Is(err, errAgentNameExists) }
func IsInvalidNameErr(err error) bool      { return errors.Is(err, errInvalidName) }
func IsInvalidProtocolErr(err error) bool  { return errors.Is(err, errInvalidProtocol) }
func IsInvalidModelErr(err error) bool     { return errors.Is(err, errInvalidModel) }
func IsInvalidTriggerErr(err error) bool   { return errors.Is(err, errInvalidTrigger) }
func IsInvalidLLMParamsErr(err error) bool { return errors.Is(err, errInvalidLLMParams) }
func IsInvalidRateLimitErr(err error) bool { return errors.Is(err, errInvalidRateLimit) }
func IsInvalidStatusErr(err error) bool    { return errors.Is(err, errInvalidStatus) }
func IsAPIKeyNotSetErr(err error) bool     { return errors.Is(err, errAPIKeyNotSet) }
func IsNoFieldsToUpdateErr(err error) bool { return errors.Is(err, errNoFieldsToUpdate) }

// 回复执行链路错误谓词（供 handler 映射手动触发接口的响应）。
func IsAgentDisabledErr(err error) bool    { return errors.Is(err, errAgentDisabled) }
func IsNotManualModeErr(err error) bool    { return errors.Is(err, errNotManualMode) }
func IsPostNotReplyableErr(err error) bool { return errors.Is(err, errPostNotReplyable) }
func IsAlreadyRepliedErr(err error) bool   { return errors.Is(err, errAlreadyReplied) }
func IsRateLimitedErr(err error) bool      { return errors.Is(err, errRateLimited) }
func IsLLMCallErr(err error) bool          { return errors.Is(err, errLLMCall) }

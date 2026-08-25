// Package domain 存放 aiagent 领域的纯领域模型：实体、值对象、常量、
// Repository 接口与哨兵错误。
//
// 依赖规则：本包不得 import 任何 gorm/redis/hertz 等基础设施或框架库，
// 也不得 import 其他领域包（domains/circle 等）。DDL 见 docs/pgsql-ddl/ai-agent.md。
package domain

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// AiAgent AI 回复机器人聚合根（表 domains.ai_agent）。
type AiAgent struct {
	ID                uuid.UUID     `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Name              string        `json:"name" gorm:"column:name;type:varchar(50);not null"`
	AvatarURL         string        `json:"avatar_url,omitempty" gorm:"column:avatar_url;type:varchar(500)"`
	LinkedUserID      uuid.UUID     `json:"linked_user_id" gorm:"column:linked_user_id;type:uuid;not null"`    // 机器人以该系统用户身份发评论
	APIProtocol       string        `json:"api_protocol" gorm:"column:api_protocol;type:varchar(20);not null"` // openai/anthropic/gemini/ollama（应用层白名单）
	BaseURL           string        `json:"base_url,omitempty" gorm:"column:base_url;type:varchar(500);not null;default:''"`
	APIKeyEnc         string        `json:"-" gorm:"column:api_key;type:varchar(512)"` // AES-GCM 密文，永不 JSON 回显
	Model             string        `json:"model" gorm:"column:model;type:varchar(100);not null"`
	LLMParams         LLMParamsJSON `json:"llm_params" gorm:"column:llm_params;type:jsonb;not null;default:'{}'::jsonb"`
	SystemPrompt      string        `json:"system_prompt,omitempty" gorm:"column:system_prompt;type:text;not null;default:''"`
	TriggerMode       int16         `json:"trigger_mode" gorm:"column:trigger_mode;type:smallint;not null;default:1"`
	TriggerKeywords   KeywordsJSON  `json:"trigger_keywords" gorm:"column:trigger_keywords;type:jsonb;not null;default:'[]'::jsonb"`
	MaxRepliesPerHour int           `json:"max_replies_per_hour" gorm:"column:max_replies_per_hour;not null;default:30"` // 0=不限
	MinIntervalSec    int           `json:"min_interval_sec" gorm:"column:min_interval_sec;not null;default:60"`         // 0=不限
	Status            int16         `json:"status" gorm:"column:status;type:smallint;not null;default:1"`
	Deleted           int16         `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime        time.Time     `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime        time.Time     `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (AiAgent) TableName() string { return "domains.ai_agent" }

// 机器人状态常量。
const (
	AgentStatusDisabled = 0 // 停用
	AgentStatusEnabled  = 1 // 启用
)

// 触发模式常量。
const (
	TriggerModeAllPost = 1 // 全部新帖
	TriggerModeKeyword = 2 // 关键词触发
	TriggerModeManual  = 3 // 手动
)

// API 协议白名单常量。
const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
	ProtocolGemini    = "gemini"
	ProtocolOllama    = "ollama"
)

// LLMParamsJSON 通用 LLM 参数（jsonb map，如 {"temperature":0.7,"top_p":1}）。
//
// 实现 sql.Scanner / driver.Valuer，语义与 post.MediaExtraJSON 一致。
type LLMParamsJSON map[string]interface{}

// Scan 实现 sql.Scanner 接口。
func (p *LLMParamsJSON) Scan(value interface{}) error {
	if value == nil {
		*p = make(LLMParamsJSON)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("LLMParamsJSON: cannot scan non-[]byte value")
	}
	if string(bytes) == "{}" {
		*p = make(LLMParamsJSON)
		return nil
	}
	return json.Unmarshal(bytes, p)
}

// Value 实现 driver.Valuer 接口。
func (p LLMParamsJSON) Value() (driver.Value, error) {
	if len(p) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(p)
}

// KeywordsJSON 触发关键词列表（jsonb string 数组）。
type KeywordsJSON []string

// Scan 实现 sql.Scanner 接口。
func (k *KeywordsJSON) Scan(value interface{}) error {
	if value == nil {
		*k = make(KeywordsJSON, 0)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("KeywordsJSON: cannot scan non-[]byte value")
	}
	if string(bytes) == "{}" {
		*k = make(KeywordsJSON, 0)
		return nil
	}
	return json.Unmarshal(bytes, k)
}

// Value 实现 driver.Valuer 接口。
func (k KeywordsJSON) Value() (driver.Value, error) {
	if len(k) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(k)
}

# AI 回复机器人域(ai-agent)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## AI 回复机器人配置表

> 全局 AI 回复机器人（agent）配置，仅管理员（`users.role = 1`）可增删改（应用层校验）。
> 机器人以 `linked_user_id` 关联的系统用户身份发表评论--创建 agent 时应用层同步创建一个
> `users` 行（如 status=1, role=2 保留段），评论外键即可复用现有 `comment.user_id` 链路。
>
> - API 协议白名单（openai/anthropic/gemini/ollama 等）由应用层维护，`api_protocol` 仅存值。
> - `api_key` 应用层加密后存密文（列长 512 预留加密膨胀）。
> - `llm_params` 存各协议通用参数，未配置项走应用层默认值；协议特有参数不入库。
> - 频率限制为「每小时上限 + 最小间隔」双阈值，执行时统计 `ai_agent_reply_log`（见下表）。

```sql
DROP TABLE IF EXISTS domains.ai_agent;

CREATE TABLE domains.ai_agent (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 机器人身份
    name VARCHAR(50) NOT NULL,               -- 机器人名称(展示名,全局唯一)
    avatar_url VARCHAR(500),                 -- 机器人头像
    linked_user_id UUID NOT NULL,            -- 关联系统用户ID(机器人以该身份发评论)

    -- 2. LLM 接入配置
    api_protocol VARCHAR(20) NOT NULL,       -- API协议: openai/anthropic/gemini/ollama(白名单应用层校验)
    base_url VARCHAR(500) NOT NULL DEFAULT '', -- API基础地址(如 https://api.openai.com/v1)
    api_key VARCHAR(512),                    -- API密钥(应用层加密存密文)
    model VARCHAR(100) NOT NULL,             -- 模型名,如 gpt-4o / claude-sonnet-5

    -- 3. LLM 通用参数
    -- 结构示例: {"temperature":0.7,"top_p":1,"max_tokens":1024,"presence_penalty":0,"frequency_penalty":0}
    llm_params JSONB NOT NULL DEFAULT '{}'::JSONB,
    system_prompt TEXT NOT NULL DEFAULT '',  -- 系统提示词(机器人人设/回复风格)

    -- 4. 触发方式
    trigger_mode SMALLINT NOT NULL DEFAULT 1, -- 触发模式: 1=全部新帖, 2=关键词触发, 3=手动
    -- trigger_mode=2 时的关键词列表,结构示例: ["AI助手","帮我看看"]
    trigger_keywords JSONB NOT NULL DEFAULT '[]'::JSONB,

    -- 5. 回复频率限制
    max_replies_per_hour INT NOT NULL DEFAULT 30, -- 每小时最大回复数(0=不限)
    min_interval_sec INT NOT NULL DEFAULT 60,     -- 两次回复最小间隔秒数(0=不限)

    -- 6. 状态与生命周期
    status SMALLINT NOT NULL DEFAULT 1,      -- 状态: 0=停用, 1=启用
    deleted SMALLINT DEFAULT 0,              -- 逻辑删除: 0=正常, 1=已删除

    -- 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.ai_agent IS 'AI回复机器人配置表(管理员维护)';
COMMENT ON COLUMN domains.ai_agent.name IS '机器人名称(展示名,未删除行内唯一)';
COMMENT ON COLUMN domains.ai_agent.linked_user_id IS '关联系统用户ID(机器人发评论的身份,创建agent时同步创建)';
COMMENT ON COLUMN domains.ai_agent.api_protocol IS 'API协议: openai/anthropic/gemini/ollama(白名单应用层校验)';
COMMENT ON COLUMN domains.ai_agent.base_url IS 'API基础地址(兼容OpenAI协议的中转/自托管端点)';
COMMENT ON COLUMN domains.ai_agent.api_key IS 'API密钥(应用层加密存密文,任何接口不得回显明文)';
COMMENT ON COLUMN domains.ai_agent.model IS '模型名';
COMMENT ON COLUMN domains.ai_agent.llm_params IS 'LLM通用参数(temperature/top_p/max_tokens/presence_penalty/frequency_penalty,未配置走应用层默认)';
COMMENT ON COLUMN domains.ai_agent.system_prompt IS '系统提示词(机器人人设/回复风格)';
COMMENT ON COLUMN domains.ai_agent.trigger_mode IS '触发模式: 1=全部新帖, 2=关键词触发, 3=手动';
COMMENT ON COLUMN domains.ai_agent.trigger_keywords IS 'trigger_mode=2时的关键词列表(JSON数组,应用层匹配)';
COMMENT ON COLUMN domains.ai_agent.max_replies_per_hour IS '每小时最大回复数(0=不限,按ai_agent_reply_log统计)';
COMMENT ON COLUMN domains.ai_agent.min_interval_sec IS '两次回复最小间隔秒数(0=不限)';
COMMENT ON COLUMN domains.ai_agent.status IS '状态: 0=停用, 1=启用';

-- --- 索引优化 ---

-- 1. 名称唯一(排除逻辑删除行)
CREATE UNIQUE INDEX idx_ai_agent_name ON domains.ai_agent(name) WHERE deleted = 0;

-- 2. 调度器扫描启用中的机器人(表小,部分索引足够)
CREATE INDEX idx_ai_agent_active ON domains.ai_agent(id) WHERE deleted = 0 AND status = 1;

-- 3. 反查机器人关联的系统用户(登录/鉴权链路排除机器人账号)
CREATE INDEX idx_ai_agent_linked_user ON domains.ai_agent(linked_user_id) WHERE deleted = 0;
```

## AI 回复日志表

> agent 每次回复的审计与频率限制统计事实表。**append-only**：无 `deleted`/`update_time`
> （失败/成功终态写入后不变，纠错靠新行）。
>
> - 频率限制执行：按 `(agent_id, create_time)` 统计最近 1 小时行数 + 取最新一行 `create_time` 算间隔。
> - 同一帖子可被同一机器人多次回复（关键词触发不设防重），用量仅靠频率限制约束。

```sql
DROP TABLE IF EXISTS domains.ai_agent_reply_log;

CREATE TABLE domains.ai_agent_reply_log (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 关联关系
    agent_id UUID NOT NULL,                  -- 机器人ID(ai_agent.id)
    post_id UUID NOT NULL,                   -- 被回复帖子ID(post.id)
    comment_id UUID,                       -- 生成的评论ID(comment.id,调用失败时为NULL)
    user_id UUID NOT NULL,                   -- 帖子作者ID(冗余,便于风控分析)

    -- 2. 调用结果
    status SMALLINT NOT NULL DEFAULT 1,      -- 结果: 0=失败, 1=成功
    error_msg VARCHAR(2048),                 -- 失败原因(截断后的错误信息)
    latency_ms INT NOT NULL DEFAULT 0,       -- LLM调用耗时(毫秒)
    prompt_tokens INT NOT NULL DEFAULT 0,    -- 输入token数(供应商返回,失败为0)
    completion_tokens INT NOT NULL DEFAULT 0, -- 输出token数(供应商返回,失败为0)

    -- 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.ai_agent_reply_log IS 'AI回复日志表(append-only,频率限制统计+审计)';
COMMENT ON COLUMN domains.ai_agent_reply_log.agent_id IS '机器人ID(ai_agent.id,无FK,应用层保证)';
COMMENT ON COLUMN domains.ai_agent_reply_log.post_id IS '被回复帖子ID(post.id,无FK,应用层保证)';
COMMENT ON COLUMN domains.ai_agent_reply_log.comment_id IS '生成的评论ID(comment.id,调用失败时为NULL)';
COMMENT ON COLUMN domains.ai_agent_reply_log.user_id IS '帖子作者ID(冗余,风控分析用)';
COMMENT ON COLUMN domains.ai_agent_reply_log.status IS '结果: 0=失败, 1=成功';
COMMENT ON COLUMN domains.ai_agent_reply_log.latency_ms IS 'LLM调用耗时(毫秒)';
COMMENT ON COLUMN domains.ai_agent_reply_log.prompt_tokens IS '输入token数(供应商返回,失败为0)';
COMMENT ON COLUMN domains.ai_agent_reply_log.completion_tokens IS '输出token数(供应商返回,失败为0)';

-- --- 索引优化 ---

-- 1. 频率限制统计: 最近1小时计数 + 最新回复时间
CREATE INDEX idx_ai_reply_agent_time ON domains.ai_agent_reply_log(agent_id, create_time DESC);
```

## 存量表迁移（comment_id 可空）

> 2026-08-25 修正：`comment_id` 原误标 `NOT NULL`，与「调用失败时为 NULL」语义矛盾（agent-reply 链路需要写失败行）。
> 已建表的存量库由 DB-owner 执行：

```sql
ALTER TABLE domains.ai_agent_reply_log ALTER COLUMN comment_id DROP NOT NULL;
```

## 存量表迁移（去除一帖一回防重）

> 2026-08-26 变更：需求调整为「同一帖子可被同一机器人多次关键词触发回复」，
> 用量仅靠频率限制（max_replies_per_hour / min_interval_sec）约束，不再设防重。
> 已建表的存量库由 DB-owner 执行：

```sql
DROP INDEX IF EXISTS domains.idx_ai_reply_unique;
```

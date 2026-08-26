# AI 机器人两阶段回复（分类器 + 生成器）设计

> **目标**：让机器人「满足特定条件才回复」从 system prompt 软约束（输出空值）升级为**两阶段硬流程**——阶段1 分类器 LLM 判定该不该回（严格 JSON 输出），判定通过才进阶段2 生成回复。
> **基线（2026-08-26 已确认决策）**：
> - A：**两阶段**而非单次结构化输出——判定调用 max_tokens 压低，不回复时 completion 成本近零；生成质量不受 JSON schema 约束污染。
> - B：分类器**复用机器人自己的 LLM 配置**（protocol/base_url/api_key/model），仅覆盖 `temperature=0, max_tokens=512`（覆盖推理模型思考 token，64 曾被烧光致空输出）；零新增凭证管理与配置项。
> - C：判定条件存 agent 新列 `filter_prompt`（自然语言，如「只回复编程相关问题」）；**空 = 跳过阶段1**，存量机器人行为完全不变（向后兼容）。
> - D：分类器**仅关键词触发（mode=2）生效**；手动触发（mode=3）是管理员显式操作，绕过判定。
> - E：判定「不回复」**写日志行 status=2（skipped）**，reason 入 `error_msg`——管理员可查「机器人为何不回复」。
> - F：限频口径排除 status=2——skipped 未产出回复，不占 `max_replies_per_hour` / `min_interval_sec` 配额。
> - G：判定输出解析失败 = **fail-closed**（不回复），按失败日志行处理（status=0）。

---

## 一、现状盘点

| 现状 | 位置 | 问题 |
|---|---|---|
| 「条件回复」靠 system prompt 让模型输出空 | `reply_service.go` executeReply 3.2-3.3 | 不回复也付全额 completion token；空输出被判 `llm returned empty content` 失败，污染失败日志 |
| 单次 LLM 调用 | `reply_service.go:344` `llm.Generate` | skip（业务决策）与 fail（调用错误）混在 status=0/1 |
| 日志状态枚举 | `domain/reply_log.go:33` | 仅 0=失败 1=成功，无「跳过」态 |
| 限频统计全部行 | `reply_log_repo_pg.go` CountSinceByAgent/GetLastByAgent | 若 skipped 入行，需排除，否则机器人被无关评论耗尽配额 |

## 二、方案选型回顾

| 方案 | 结论 |
|---|---|
| 单次调用 + 结构化 JSON | 否：JSON 约束影响生成质量；跨协议实现不一（openai response_format vs anthropic tool use） |
| **两阶段：分类器 + 生成器** | **是**：判定成本极低，生成路径零侵入 |
| 代码规则前置 + LLM 语义兜底 | 部分采纳：关键词匹配已是代码前置；「编程问题」类语义规则必须 LLM，入 filter_prompt |

## 三、最终方案

### 3.1 执行规则（在 agent-reply-design §3.1 基础上增量）

| 规则 | mode=2 关键词 | mode=3 手动 |
|---|---|---|
| 分类器判定 | `filter_prompt` 非空时：限频过后、生成之前调 LLM 判定 | **不过判定**（trigger==nil 天然绕过） |
| 判定参数 | 复用 agent 的 protocol/base_url/api_key/model，覆盖 `temperature=0, max_tokens=512` | — |
| 判定不通过 | 写 status=2 日志行（reason 入 error_msg），静默结束 | — |
| 判定解析失败 | fail-closed：写 status=0 失败行「classifier parse failed」 | — |
| 限频 | 统计口径排除 status=2 | 同左 |

> 「跳过记日志」边界调整：前置条件不满足（帖子不可回/限频）仍**不记日志**；分类器跳过**记日志**——因为它消耗了真实 LLM 调用，且 reason 是排查「机器人为何不回复」的关键证据。

### 3.2 判定 Prompt 组装

```
system = 固定模板（judgeSystemPrompt 常量）：
  「你是内容分类器。根据判定条件决定是否应回复。
   只输出 JSON：{"reply":true|false,"reason":"一句话原因"}，禁止任何其他输出。」
user   = "帖子标题：{title}\n帖子摘要：{summary}\n用户评论：{comment_content}\n判定条件：{filter_prompt}"
```
- 各段复用 `truncateRunes` + `aiagent.max_content_chars` 截断。
- 判定调用与生成调用共享 executeReply 的 ctx 超时（判定 max_tokens=512，秒级返回，不新增超时配置）。

### 3.3 判定结果解析（parseJudgeResult）

- 剥 ```` ```json ```` / ```` ``` ```` markdown fence → `json.Unmarshal` 到 `{Reply bool, Reason string}`。
- 任何解析失败 → fail-closed（不回复 + 失败日志行），宁可漏回不可乱回。

### 3.4 Token 与日志口径

- reply_log 无新列：`prompt_tokens`/`completion_tokens` = 两阶段**求和**；skipped 行只含分类器消耗。
- `latency_ms` = 进入调用链路起的总耗时（含两阶段）。

## 四、数据流

```
评论创建 → OnCommentCreated（异步）
  → 防回环 → ListEnabled → mode=2? → 关键词命中? → 信号量
  → executeReply:
      1. 帖子门槛（status=1 且未锁定）          不过 → 静默跳过
      2. 限频（排除 status=2 的行）             不过 → 静默跳过
      3. filter_prompt 非空?
         ├ 空  → 直接进 4（向后兼容）
         └ 非空 → 分类器 LLM 判定
                  ├ 调用/解析失败 → status=0 失败行，结束
                  ├ reply=false  → status=2 skipped 行（reason），结束
                  └ reply=true   → 累计 token，进 4
      4. 生成 LLM → SanitizeForPg → 空=失败行
      5. CreateComment（挂触发评论楼层）→ status=1 成功行（token=两阶段和）
```

## 五、Schema/配置变更

- `domains.ai_agent` 加列 `filter_prompt TEXT NOT NULL DEFAULT ''`（详见 docs/pgsql-ddl/ai-agent.md 迁移段）。
- `domains.ai_agent_reply_log.status` 注释扩展：`0=失败, 1=成功, 2=分类器跳过`（无结构变更）。
- 配置：**无新增**，复用 `aiagent.timeout_sec / max_content_chars / reply_concurrency`。

## 六、一致性/边界/风险

| 项 | 处理 |
|---|---|
| 存量机器人 | `filter_prompt=''` → 不过判定，行为与旧版完全一致 |
| 分类器幻觉/误判 | fail-open 方向不存在：判定通过才回复，误判只会「多回」不会「乱回内容」；解析失败 fail-closed |
| 判定成本 | max_tokens=512 + temperature=0，单次判定 completion ≈ 几十个 token；解析失败兜底正则取 reply 字段 |
| 限频绕过 | skipped 不计配额；但分类器本身消耗 token，靠 reply_concurrency 信号量与关键词前置匹配兜底 |
| 超时 | 两阶段共享 executeReply ctx；分类器过慢会挤占生成时间片，超时即失败行（可接受，判定理应秒回） |
| mode=1（全帖触发） | P2 实现时同样过分类器（trigger!=nil 即可，天然兼容） |

## 七、分阶段交付

| 阶段 | 内容 |
|---|---|
| P0（本次） | filter_prompt 列 + CRUD 透传；executeReply 两阶段；status=2；限频排除 skipped；parseJudgeResult 单测 |
| P1 | 回复日志管理端查询 API（顺带暴露 status=2 给管理员排查） |

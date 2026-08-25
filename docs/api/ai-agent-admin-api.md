# AI 回复机器人管理端 API 对接文档（前端）

> **功能**：管理员（`users.role=1`）对 AI 回复机器人（agent）的增删改查。
> **对应实现**：`pkg/domains/aiagent/`，DDL 见 [../pgsql-ddl/ai-agent.md](../pgsql-ddl/ai-agent.md)。
> **状态**：CRUD 已交付。机器人的实际回复执行链路（消费新帖 → 调 LLM → 发评论）后续单独交付，本期无相关接口。

---

## 0. TL;DR -- 前端四件事

1. **全部接口需登录 + 管理员**：请求头带 `satoken: <token>`；非管理员统一返回 **403**（不是 401）。
2. **api_key 只写不读**：创建/更新时提交明文，任何读接口只回掩码（`sk-****z789`），**永远拿不到明文/密文**。
3. **更新是部分更新**：PUT 请求体只传要改的字段（指针语义），未传字段不动。
4. **删除是软删**：`DELETE` 后列表/详情即不可见，**无恢复接口**；误删只能重建。

---

## 1. 鉴权与通用约定

### 1.1 请求头

```
satoken: <登录成功后返回的 token>
Content-Type: application/json   （POST/PUT）
```

### 1.2 鉴权失败两种情况

| 场景 | HTTP 状态 | 业务 code | 说明 |
|---|---|---|---|
| 未登录 / token 无效 | 401 | 401 | 同全站逻辑 |
| 已登录但 role≠1 | 403 | 403 | `Admin role required` |

**前端建议**：管理后台页面入口先自查 role（`/user/get` 返回的 `role` 字段），非 1 直接隐藏入口，省一次 403 往返。

### 1.3 统一响应信封

```json
{ "code": 200, "message": "Success", "data": { ... } }
```

列表接口额外带分页字段（见 §3）。

---

## 2. 数据模型（AgentVO）

所有读接口（详情/列表项/创建与更新的返回值）结构一致：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(UUIDv7) | 机器人 ID |
| `name` | string | 展示名，全局唯一，1-50 字符 |
| `avatar_url` | string | 头像，可空 |
| `linked_user_id` | string(UUID) | 关联系统用户 ID（机器人以该身份发评论；**只读**，创建时后端自动生成 role=2 机器人账号） |
| `api_protocol` | string | 协议：`openai` / `anthropic` / `gemini` / `ollama` |
| `base_url` | string | 自定义 API 地址，可空（用官方默认端点时留空） |
| `has_api_key` | bool | 是否配置了 key |
| `api_key_masked` | string | key 掩码（如 `sk-****z789`），未配置时无此字段 |
| `model` | string | 模型名，1-100 字符（如 `gpt-4o-mini` / `claude-sonnet-5`） |
| `llm_params` | object | 通用 LLM 参数，见 §2.1 |
| `system_prompt` | string | 系统提示词，可空 |
| `trigger_mode` | int | 触发模式：1=全部新帖，2=关键词触发，3=手动 |
| `trigger_keywords` | string[] | 触发关键词；仅 mode=2 时有意义 |
| `max_replies_per_hour` | int | 每小时回复上限，0=不限，默认 30 |
| `min_interval_sec` | int | 两次回复最小间隔秒，0=不限，默认 60 |
| `status` | int | 1=启用，0=停用 |
| `create_time` / `update_time` | string(RFC3339) | 时间戳 |

### 2.1 llm_params 白名单

只接受以下键，值为**数字**，其余一律 400：

```json
{ "temperature": 0.7, "top_p": 1, "max_tokens": 1024, "presence_penalty": 0, "frequency_penalty": 0 }
```

数值范围后端不硬校验（交给供应商报错），前端自行用输入框约束即可。

### 2.2 响应示例

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "id": "0192a3f0-5b40-7000-8000-0000000000ab",
    "name": "小吧机器人",
    "avatar_url": "https://cdn.qubar.site/avatar/bot.png",
    "linked_user_id": "0192a3f0-5b40-7000-8000-0000000000cd",
    "api_protocol": "openai",
    "base_url": "",
    "has_api_key": true,
    "api_key_masked": "sk-****z789",
    "model": "gpt-4o-mini",
    "llm_params": { "temperature": 0.7, "max_tokens": 1024 },
    "system_prompt": "你是兴趣社区的吧务助手，回复要简短友好。",
    "trigger_mode": 2,
    "trigger_keywords": ["怎么加入", "求助", "新人"],
    "max_replies_per_hour": 30,
    "min_interval_sec": 60,
    "status": 1,
    "create_time": "2026-08-25T10:00:00+08:00",
    "update_time": "2026-08-25T10:00:00+08:00"
  }
}
```

---

## 3. 接口清单

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/agent` | 创建机器人 |
| GET | `/agent/list` | 列表（offset 分页） |
| GET | `/agent/:id` | 详情 |
| PUT | `/agent/:id` | 部分更新 |
| DELETE | `/agent/:id` | 软删（停用） |

### 3.1 创建机器人

`POST /agent`

**请求体**（全部字段同上层语义；`api_key` 明文提交，仅 HTTPS 环境使用）：

```json
{
  "name": "小吧机器人",
  "avatar_url": "https://cdn.qubar.site/avatar/bot.png",
  "api_protocol": "openai",
  "base_url": "",
  "api_key": "sk-xxxxx（ollama 本地协议可省略）",
  "model": "gpt-4o-mini",
  "llm_params": { "temperature": 0.7, "max_tokens": 1024 },
  "system_prompt": "你是兴趣社区的吧务助手，回复要简短友好。",
  "trigger_mode": 2,
  "trigger_keywords": ["怎么加入", "求助"],
  "max_replies_per_hour": 30,
  "min_interval_sec": 60
}
```

**默认值**（字段不传时）：`trigger_mode=1`、`max_replies_per_hour=30`、`min_interval_sec=60`、`status=1`、`llm_params={}`、`trigger_keywords=[]`。

**成功**：HTTP 200，`data` 为完整 AgentVO（注意本项目创建统一回 200/`Created successfully`，非 201）。

**注意**：
- `trigger_mode=2` 时 `trigger_keywords` 必填非空，否则 400。
- `api_key` 允许为空串（ollama 免 key）。
- **创建即停用做不到**：int 字段无法区分「未传」与「0」，不传 `status` 一律默认启用；要先停用，创建后立刻 `PUT` 改 `status=0`。
- 创建成功即自动创建 role=2 机器人系统用户（`linked_user_id`），前端无需也不应传该字段。

### 3.2 列表

`GET /agent/list?page=1&size=20&keyword=小吧`

- `page` 默认 1；`size` 默认 20，上限 100（越界回落 20）。
- `keyword` 可选，按机器人 `name` 模糊匹配（ILIKE，大小写不敏感，两端空格忽略）；不传或为空 = 全量。
- 排序固定 `create_time` 倒序。
- `total` 为**过滤后**的总数，直接用于分页组件。

**响应**（分页信封）：

```json
{
  "code": 200,
  "message": "Success",
  "data": [ { ...AgentVO }, { ...AgentVO } ],
  "total": 2,
  "page": 1,
  "per_page": 20
}
```

### 3.3 详情

`GET /agent/:id`

路径参数为机器人 UUID。不存在/已删除返回 404（`Agent not found`）。

### 3.4 更新（部分更新）

`PUT /agent/:id`

**只传要改的字段**，未传字段保持原值。例——只换 key 和停用：

```json
{ "api_key": "sk-new-key", "status": 0 }
```

可更新字段：`name`、`avatar_url`、`api_protocol`、`base_url`、`api_key`、`model`、`llm_params`、`system_prompt`、`trigger_mode`、`trigger_keywords`、`max_replies_per_hour`、`min_interval_sec`、`status`。

**注意**：
- `api_key` 传**空串 = 清除 key**（`has_api_key` 变 false）；要「保持不变」就不传该字段。
- `llm_params` / `trigger_keywords` 是**整体替换**语义（传了就覆盖全量），不是合并。
- `trigger_mode` 改为 2 时，同请求需带非空 `trigger_keywords`（或已存关键词非空）。
- 空请求体（无任何可更新字段）返回 400（`at least one field to update`）。
- `name` 改名会做唯一性检查，撞名返回 409。

### 3.5 删除

`DELETE /agent/:id`

软删：`deleted=1` 且自动停用。成功响应 `data` 为空。

**不可恢复**（无恢复接口）；其关联机器人账号与历史回复保留。

---

## 4. 错误码速查

| HTTP | message | 场景 | 前端处理建议 |
|---|---|---|---|
| 400 | `agent name must be 1-50 chars` | 名称为空/超长 | 表单校验 |
| 400 | `api_protocol must be openai/anthropic/gemini/ollama` | 协议不在白名单 | 用下拉框 |
| 400 | `model must be 1-100 chars` | 模型名为空/超长 | 表单校验 |
| 400 | `trigger_mode must be 1/2/3; mode 2 requires keywords` | 模式非法 / mode=2 无关键词 | 联动显隐关键词输入 |
| 400 | `llm_params has invalid key/value` | 参数键不在白名单 / 值非数字 | 固定表单字段 |
| 400 | `rate limit values must be >= 0` | 限频字段为负 | 数字输入框 min=0 |
| 400 | `status must be 0 or 1` | status 非法 | 开关组件 |
| 400 | `at least one field to update` | PUT 空更新 | 提交前检查 |
| 400 | `Invalid agent id` | 路径 :id 非 UUID | 不会出现，防御 |
| 401 | `Token not found` / `Invalid or expired token` | 未登录/token 过期 | 走全局 401 拦截 |
| 403 | `Admin role required` | 登录但非管理员 | 提示无权限 |
| 404 | `Agent not found` | 不存在/已删除 | 返回列表页 |
| 409 | `Agent name already exists` | 名称重复（创建/改名） | 表单报错提示换名 |
| 503 | `Data key not configured` | 后端未配 `security.data_key` 且提交了 api_key | 后端配置问题，联系后端 |

未知错误统一 500（`Internal server error`）。

---

## 5. 对接注意事项汇总

1. **key 安全**：明文 key 只在 POST/PUT 请求体出现一次；编辑页回显用 `api_key_masked` + `has_api_key`，「换 key」用独立输入框，不回填旧值。
2. **掩码含义**：`sk-****z789` = 前 3 + `****` + 后 4；key 长度 ≤8 时显示 `****`。掩码只用于人工辨认，不可用于任何提交。
3. **列表搜索**：`GET /agent/list` 支持 `keyword` 按 `name` 模糊过滤（服务端 ILIKE，`total` 为过滤后总数）；也可继续本地过滤。
4. **`linked_user_id` 只读**：机器人账号由后端生成（role=2，username/email 由 uuidv7+时间戳生成，明显非真人），前端仅展示。
5. **status 语义**：停用（0）后机器人不再参与回复触发（执行链路上线后生效）；CRUD 层面停用仅是标记。
6. **触发/限频字段本期为「配置存储」**：`trigger_mode`/`trigger_keywords`/限频字段已可配置并校验，但实际回复执行链路未上线，配置暂不产生效果。
7. **时间字段**：RFC3339 带时区（+08:00），直接 `new Date()` 解析即可。
8. **并发编辑**：无版本控制，后写覆盖；管理后台单人使用场景可接受。

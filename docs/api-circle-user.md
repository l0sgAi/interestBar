# 查看任意用户加入圈子接口（GET /circle/user）

## 概述

返回**任意指定用户**已加入的兴趣圈列表，支持对圈子 `name` / `description` 做关键字模糊查询，支持 `search_after` 游标分页。

与 [`GET /circle/my`](../pkg/domains/circle/interfaces/http/handler.go) 的核心区别：

| 维度 | `/circle/my` | `/circle/user` |
|---|---|---|
| 数据范围 | **当前登录用户**加入的圈子 | **任意指定用户**加入的圈子 |
| 目标用户来源 | 会话 token | 请求参数 `user_id` |
| 鉴权 | 需登录 | 需登录（任意已登录用户均可查看他人加入的圈子）|

> 圈子成员关系默认公开，任何已登录用户均可通过本接口查看他人加入的圈子。

## 请求

```
GET /circle/user
```

### 鉴权

需要登录。请求头携带 token：

```
satoken: <登录返回的 token>
```

> 请求头名称由配置项 `sa_token.token_name` 决定，当前为 `satoken`。未携带或 token 失效返回 `401`。

### Query 参数

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `user_id` | string(uuid) | **是** | — | 目标用户 ID。非法（非 UUID）返回 `400` |
| `keyword` | string | 否 | `""` | 关键字，模糊匹配圈子 `name`（权重 ×3）与 `description`（权重 ×1）。为空时进入浏览模式，返回该用户加入的全部圈子（按加入时间倒序分页）|
| `size` | int | 否 | `20` | 每页数量。`<=0` 或 `>100` 时回退为 `20` |
| `search_after` | string | 否 | `""` | 上一页响应返回的 `search_after` 游标。**base64 不透明串，原样透传，不要解析或修改** |

## 响应

### 外层结构（标准响应包）

```jsonc
{
  "code": 200,                 // 业务码：200 成功
  "message": "Success",
  "data": { ... }              // 见下
}
```

### `data` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `circles` | `MyCircleDoc[]` | 圈子列表 |
| `total` | `int64` | **浏览模式**（`keyword` 为空）：该用户加入的圈子总数；**搜索模式**（`keyword` 非空）：固定为 `0`（未知，见下「total 语义」）|
| `size` | `int` | 每页数量 |
| `search_after` | `string` | 下一页游标。**为空字符串表示已到末页**，无更多数据 |
| `truncated` | `bool` | 仅搜索模式可能为 `true`：表示服务端扫描到上限仍未集齐 `size` 条，可能还有更深的命中未返回（见下「truncated 语义」）。浏览模式恒不出现该字段 |

### `MyCircleDoc` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string`(uuid) | 圈子 ID |
| `name` | `string` | 圈子名称 |
| `avatar_url` | `string` | 圈子头像 URL，无头像时字段缺省（`omitempty`） |
| `member_count` | `int` | 圈子成员数 |

## 分页说明（重要）

### 游标分页，非 offset

本接口用 **`search_after` 游标**分页，**不是** `page`/`offset`。

- **首页**：不传 `search_after`（或传空串）。
- **下一页**：把上一页响应里的 `search_after` **原样**作为请求参数传入。
- **末页**：响应 `search_after` 为空字符串时，表示无更多数据，停止翻页。

```text
请求1: GET /circle/user?user_id=xxx&size=20
       → data.search_after = "<cursor_1>"
请求2: GET /circle/user?user_id=xxx&size=20&search_after=<cursor_1>
       → data.search_after = "<cursor_2>"
请求3: GET /circle/user?user_id=xxx&size=20&search_after=<cursor_2>
       → data.search_after = ""   ← 末页，停止
```

> ⚠️ `search_after` 是服务端生成的不透明 base64 串，**禁止前端解析、拼接、缓存复用**。每次翻页都用上一次响应原值。篡改或格式错误返回 `400 Invalid search_after parameter`。

### total 语义

| 模式 | `total` 含义 | 前端展示建议 |
|---|---|---|
| 浏览（`keyword` 空）| 该用户加入圈子总数（准确）| 可展示「共加入 N 个圈子」 |
| 搜索（`keyword` 非空）| **固定 `0`**（服务端按批次流式扫描，不预知总命中数）| **不要展示总数**；用 `search_after` 是否为空判断「还有没有更多」|

### truncated 语义（仅搜索模式）

服务端搜索模式会按加入时间倒序**分批扫描**该用户的圈子（每批 100 个，单次请求最多扫 5000 个）。若扫满 5000 仍未集齐 `size` 条命中：

- `truncated = true`：可能还有更早加入、但匹配关键字的圈子未被返回。
- `search_after` 仍可能非空：前端可继续用它翻页（下一批请求从上次扫描位置继续，再扫最多 5000 个）。

前端处理建议：

- `truncated === true` 时可提示「结果可能不全，请细化关键字」。
- 也可选择继续翻页加载，直到 `search_after` 为空。

浏览模式不存在截断，`truncated` 字段不会出现。

### 排序

- **浏览模式**：按加入时间倒序分页（最近加入的圈子在前）。
- **搜索模式**：在加入时间倒序的扫描顺序上，按关键字相关度命中。列表项顺序由服务端决定，前端按返回顺序渲染即可。

## 错误码

| HTTP | 业务码 | message | 触发条件 |
|---|---|---|---|
| 400 | `CodeBadRequest` | `Invalid user_id` | `user_id` 缺失或非合法 UUID |
| 400 | `CodeBadRequest` | `Invalid request parameters` | query 参数绑定失败 |
| 400 | `CodeBadRequest` | `Invalid search_after parameter` | `search_after` 格式非法（被篡改/非服务端原值）|
| 401 | `CodeUnauthorized` | `Token not found` | 未登录 / token 失效 |
| 500 | `CodeInternalError` | `Internal error` | 服务端异常（缓存/ES 故障等）|

> **不存在的用户**：`user_id` 是合法 UUID 但系统无此用户、或该用户未加入任何圈子时，**不返回 404**，而是返回空列表：`circles: [], total: 0, search_after: ""`。

## 示例

### 1. 浏览模式 — 取首页

```bash
curl -G 'https://api.example.com/circle/user' \
  -H 'satoken: <token>' \
  --data-urlencode 'user_id=0192a3b4-c5d6-7e8f-9001-234567890abc' \
  --data-urlencode 'size=20'
```

响应：

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "circles": [
      {
        "id": "0192a3b4-aaaa-bbbb-cccc-dddddddddddd",
        "name": "摄影爱好者",
        "avatar_url": "https://cdn.example.com/circles/photo.png",
        "member_count": 1280
      },
      {
        "id": "0192a3b4-eeee-ffff-0000-111111111111",
        "name": "户外徒步",
        "member_count": 864
      }
    ],
    "total": 42,
    "size": 20,
    "search_after": "eyJyIjoyMH0"
  }
}
```

### 2. 翻下一页

```bash
curl -G 'https://api.example.com/circle/user' \
  -H 'satoken: <token>' \
  --data-urlencode 'user_id=0192a3b4-c5d6-7e8f-9001-234567890abc' \
  --data-urlencode 'size=20' \
  --data-urlencode 'search_after=eyJyIjoyMH0'
```

> `search_after` 原样取上一页响应值。当响应 `search_after` 为 `""` 时停止翻页。

### 3. 搜索模式 — 关键字过滤

```bash
curl -G 'https://api.example.com/circle/user' \
  -H 'satoken: <token>' \
  --data-urlencode 'user_id=0192a3b4-c5d6-7e8f-9001-234567890abc' \
  --data-urlencode 'keyword=摄影' \
  --data-urlencode 'size=20'
```

响应：

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "circles": [
      {
        "id": "0192a3b4-aaaa-bbbb-cccc-dddddddddddd",
        "name": "摄影爱好者",
        "avatar_url": "https://cdn.example.com/circles/photo.png",
        "member_count": 1280
      }
    ],
    "total": 0,            // ← 搜索模式固定 0，勿展示总数
    "size": 20,
    "search_after": ""     // ← 已无更多，停止翻页
    // truncated 字段不出现 = 未截断
  }
}
```

若命中很多且扫到上限：

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "circles": [ /* 20 条 */ ],
    "total": 0,
    "size": 20,
    "search_after": "eyJyIjo1MDAwfQ",
    "truncated": true       // ← 提示「结果可能不全，请细化关键字」或继续翻页
  }
}
```

## 前端对接 Checklist

- [ ] `user_id` 必填，从用户主页/路由参数取，确保是合法 UUID。
- [ ] `search_after` 当作**不透明字符串**，首页不传，后续页原样回传上一页响应值；空串 = 末页。
- [ ] **浏览模式**（无 keyword）才展示 `total`；**搜索模式**（有 keyword）`total` 恒 0，**不展示总数**，用 `search_after` 是否非空判断「加载更多」。
- [ ] 搜索模式留意 `truncated`：为 `true` 时提示「结果可能不全」或继续翻页。
- [ ] 翻页停止条件：响应 `search_after === ""`。
- [ ] 目标用户不存在 / 未加圈子 → 空列表，非 404，前端按空状态处理。

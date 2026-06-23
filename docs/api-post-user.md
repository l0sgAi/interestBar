# 查看任意用户发帖接口（GET /post/user/:user_id）

## 概述

返回**任意指定用户**已发布的帖子列表，供「访问他人主页 / 查看某人发帖记录」场景使用。支持对 `title` / `summary` 做关键字模糊查询（容错拼写错误），支持 `search_after` 游标分页。

与 [`GET /post/my`](api-post-my.md) 的核心区别：

| 维度 | `/post/my` | `/post/user/:user_id` |
|---|---|---|
| 数据范围 | **仅当前登录用户自己** | **任意指定用户**（路径参数 `:user_id`） |
| 调用者与目标 | 本人看自己 | 他人（或本人）看目标用户 |
| status 过滤 | **不过滤**，作者可见全部状态（草稿/审核/已发布/拒绝/封禁），仅排除已删除 | **强制 `status=1`（仅已发布）**，排除草稿/审核/拒绝/封禁/已删除 |
| 关键字匹配 | `title` / `summary` 分词匹配 + `fuzziness:AUTO` | 同左 |

> 隐私语义：查看者只能看到目标用户**已发布**的帖子。对方的草稿、审核中、已拒绝、已封禁帖**一律不可见**。若需查看自己的全部状态（含草稿），请用 [`/post/my`](api-post-my.md)。

## 请求

```
GET /post/user/:user_id
```

### 鉴权

需要登录。请求头携带 token：

```
satoken: <登录返回的 token>
```

> 请求头名称由配置项 `sa_token.token_name` 决定，当前为 `satoken`。未携带或 token 失效返回 `401`。

### 路径参数

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `user_id` | string(uuid) | 是 | 目标用户 ID。非法 UUID 返回 `400 Invalid user_id` |

### Query 参数

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `keyword` | string | 否 | `""` | 关键字，模糊匹配 `title`（权重 ×3）与 `summary`（权重 ×1）。为空时返回该用户全部已发布帖子，按 `id` 倒序 |
| `size` | int | 否 | `20` | 每页数量。`<=0` 或 `>100` 时回退为 `20` |
| `search_after` | string | 否 | `""` | 上一页响应返回的 `search_after` 值，用于取下一页。**原样透传，不要修改** |

## 响应

### 外层结构（标准响应包）

```jsonc
{
  "code": 200,                 // 业务码：200 成功
  "message": "Success",
  "data": { ... }              // PostSearchResult，见下
}
```

### `data` 字段（PostSearchResult）

| 字段 | 类型 | 说明 |
|---|---|---|
| `posts` | `PostListItem[]` | 帖子列表（已组装作者/圈子/图片信息） |
| `total` | `int64` | 命中总数 |
| `size` | `int` | 本页返回条数 |
| `search_after` | `string` | 下一页游标。**为空字符串表示已到末页**，无更多数据 |

### `PostListItem` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string`(uuid) | 帖子 ID |
| `circle_id` | `string`(uuid) | 所属圈子 ID |
| `user_id` | `string`(uuid) | 发帖人 ID（本接口恒为路径传入的目标用户） |
| `type` | `int` | 帖子类型：`1`=图文 / `2`=视频 / `3`=投票 |
| `title` | `string` | 标题 |
| `summary` | `string` | 摘要 |
| `content` | `string` | 正文 |
| `view_count` | `int` | 浏览量 |
| `comment_count` | `int` | 评论数 |
| `like_count` | `int` | 点赞数 |
| `collect_count` | `int` | 收藏数 |
| `is_pinned` | `int16` | 是否置顶：`0` 否 / `1` 是 |
| `is_essence` | `int16` | 是否精华：`0` 否 / `1` 是 |
| `is_lock` | `int16` | 是否锁定：`0` 否 / `1` 是 |
| `status` | `int16` | 帖子状态：本接口恒为 `1`（已发布） |
| `create_time` | `string` | 发帖时间（RFC3339Nano） |
| `author_name` | `string` | 作者昵称 |
| `author_avatar` | `string` | 作者头像 URL |
| `circle_name` | `string` | 所属圈子名 |
| `circle_avatar` | `string` | 所属圈子头像 URL |
| `images` | `string[]` | 帖子图片 URL 列表 |

> 字段结构与 [`/post/my`](api-post-my.md) 完全一致，前端可复用同一渲染逻辑。

## 分页说明（search_after）

采用 ES `search_after` 游标分页，**非传统 offset/page**：

1. 首次请求**不带** `search_after`。
2. 从响应取 `data.search_after`（一个 JSON 数组字符串）。
3. 取下一页时，把它**原样**作为 `?search_after=` 传入。
4. 当响应 `search_after` 为空字符串 `""` 时，表示已到末页，停止翻页。

> 排序规则：有关键字时按相关度（`_score`）降序，其次 `id` 降序；无关键字时按 `id` 降序（最新在前）。游标与排序绑定，翻页过程中不要更换 `keyword`/`size`。

## 示例

### 1. 浏览某用户的全部已发布帖（首页）

用 B 的 token 查看 A 的发帖记录：

```bash
curl -G "https://<host>/post/user/0190aaaa-...-..." \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "size=10"
```

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "posts": [
      {
        "id": "0192f8a1-...-...",
        "circle_id": "0191ab...-...",
        "user_id": "0190aaaa-...-...",
        "type": 1,
        "title": "今日穿搭分享",
        "summary": "初秋通勤穿搭，三套搭配思路...",
        "content": "...",
        "view_count": 128,
        "comment_count": 12,
        "like_count": 45,
        "collect_count": 8,
        "is_pinned": 0,
        "is_essence": 1,
        "is_lock": 0,
        "status": 1,
        "create_time": "2026-06-20T10:23:45.123456789Z",
        "author_name": "小洛",
        "author_avatar": "https://cdn.../avatar.png",
        "circle_name": "穿搭日记",
        "circle_avatar": "https://cdn.../c.png",
        "images": ["https://cdn.../1.jpg", "https://cdn.../2.jpg"]
      }
    ],
    "total": 1,
    "size": 10,
    "search_after": "[\"0192f8a1-...-...\"]"
  }
}
```

### 2. 关键字搜索（含下一页）

```bash
# 第一页
curl -G "https://<host>/post/user/0190aaaa-...-..." \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "keyword=穿搭" \
  --data-urlencode "size=10"

# 第二页（search_after 取上一页响应值）
curl -G "https://<host>/post/user/0190aaaa-...-..." \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "keyword=穿搭" \
  --data-urlencode "size=10" \
  --data-urlencode 'search_after=["0192f8a1-...-..."]'
```

> `keyword` 支持拼写容错（`fuzziness:AUTO`）。例如标题是「穿搭分享」，搜「穿搭」也可能命中。中文分词为主，容错对英文/拼音输入更明显。

### 3. 无匹配 / 该用户无已发布帖

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": { "posts": [], "total": 0, "size": 20, "search_after": "" }
}
```

### 4. 已到末页

响应 `search_after` 为空字符串 `""` → 不再请求下一页。

## 错误码

| HTTP | `code` | `message` | 触发场景 |
|---|---|---|---|
| 401 | 202 | `Token not found` | 未携带 `satoken` 或未登录 |
| 401 | 202 | `Invalid or expired token` | token 失效 |
| 400 | 201 | `Invalid user_id` | 路径 `:user_id` 不是合法 UUID |
| 400 | 201 | `Invalid search_after parameter` | `search_after` 不是合法 JSON 数组 |
| 500 | 210 | `Failed to search user posts` | ES 查询异常（服务端日志见 `"Failed to search user posts"`） |

> 错误响应体：`{ "code": <code>, "message": "<msg>" }`（无 `data` 字段）。

## 注意事项 / 已知边界

1. **草稿不可见是设计如此**：本接口强制 `status=1`，目标用户的草稿/审核中/已拒绝/已封禁帖**对查看者不可见**。这**不是** ES 索引时机问题，而是隐私规则。若需查看自己的草稿，用 [`/post/my`](api-post-my.md)。
2. **依赖 ES 已发布索引**：本接口数据完全来自 Elasticsearch 帖子索引。若某条已发布帖尚未同步入索引（发帖流程异步落库），会有秒级延迟，期间查不到。
3. **数据为查询时刻的快照**：`view_count` / `like_count` 等计数取自 ES 文档，可能有秒级同步延迟（经聚合器异步落库）。
4. **`user_id` 恒为目标用户**：本接口已按路径 `:user_id` 过滤，返回项里的 `user_id` 必为该目标用户，可省略二次过滤。查询本人时（`:user_id` = 当前登录用户）等价于「只看自己已发布帖」。
5. **路由前缀**：若网关有全局前缀（如 `/api/v1`），实际路径为 `/api/v1/post/user/:user_id`，以前端实际部署为准。

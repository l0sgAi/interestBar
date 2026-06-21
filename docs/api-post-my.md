# 查看自己发帖接口（GET /post/my）

## 概述

返回**当前登录用户**自己发布的帖子列表，支持对 `title` / `summary` 做关键字模糊查询（容错拼写错误），支持 `search_after` 游标分页。

与 [`GET /post/list`](../pkg/domains/post/interfaces/http/handler.go) 的核心区别：

| 维度 | `/post/list` | `/post/my` |
|---|---|---|
| 数据范围 | 全站（可按 circle_id 过滤） | **仅当前登录用户自己的帖子** |
| status 过滤 | 仅 `status=1`（已发布） | **不过滤**，作者可见全部状态（草稿/审核/已发布/拒绝/封禁），仅排除已删除 |
| 关键字匹配 | `title` / `summary` 分词匹配 | 同左 + `fuzziness:AUTO`（容忍拼写错误） |

## 请求

```
GET /post/my
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
| `keyword` | string | 否 | `""` | 关键字，模糊匹配 `title`（权重 ×3）与 `summary`（权重 ×1）。为空时返回该用户全部帖子，按 `id` 倒序 |
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
| `user_id` | `string`(uuid) | 发帖人 ID（本接口恒为当前登录用户） |
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
| `status` | `int16` | 帖子状态：见下表 |
| `create_time` | `string` | 发帖时间（RFC3339Nano） |
| `author_name` | `string` | 作者昵称 |
| `author_avatar` | `string` | 作者头像 URL |
| `circle_name` | `string` | 所属圈子名 |
| `circle_avatar` | `string` | 所属圈子头像 URL |
| `images` | `string[]` | 帖子图片 URL 列表 |

### `status` 枚举

| 值 | 含义 |
|---|---|
| `0` | 草稿 |
| `1` | 已发布 |
| `2` | 审核中 |
| `3` | 已拒绝 |
| `4` | 已封禁 |

## 分页说明（search_after）

采用 ES `search_after` 游标分页，**非传统 offset/page**：

1. 首次请求**不带** `search_after`。
2. 从响应取 `data.search_after`（一个 JSON 数组字符串）。
3. 取下一页时，把它**原样**作为 `?search_after=` 传入。
4. 当响应 `search_after` 为空字符串 `""` 时，表示已到末页，停止翻页。

> 排序规则：有关键字时按相关度（`_score`）降序，其次 `id` 降序；无关键字时按 `id` 降序（最新在前）。游标与排序绑定，翻页过程中不要更换 `keyword`/`size`。

## 示例

### 1. 浏览我的全部帖子（首页）

```bash
curl -G "https://<host>/post/my" \
  -H "satoken: eyJhbGciOi..." \
  -G --data-urlencode "size=10"
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
        "user_id": "0190...-...",
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
curl -G "https://<host>/post/my" \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "keyword=穿搭" \
  --data-urlencode "size=10"

# 第二页（search_after 取上一页响应值）
curl -G "https://<host>/post/my" \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "keyword=穿搭" \
  --data-urlencode "size=10" \
  --data-urlencode 'search_after=["0192f8a1-...-..."]'
```

> `keyword` 支持拼写容错（`fuzziness:AUTO`）。例如标题是「穿搭分享」，搜「穿搭」也可能命中。中文分词为主，容错对英文/拼音输入更明显。

### 3. 无匹配

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
| 400 | 201 | `Invalid search_after parameter` | `search_after` 不是合法 JSON 数组 |
| 500 | 211 | `Failed to search my posts` | ES 查询异常（服务端日志见 `"Failed to search my posts"`） |

> 错误响应体：`{ "code": <code>, "message": "<msg>" }`（无 `data` 字段）。

## 注意事项 / 已知边界

1. **草稿可见性依赖 ES 索引**：本接口数据完全来自 Elasticsearch 帖子索引。若发帖流程仅在帖子进入「已发布」状态时才写入 ES，则**草稿/审核中**的帖子在本接口**查不到**。若产品要求「我的草稿」也可见，需另加 DB 兜底查询，或保证草稿同样入索引。接入前请与后端确认发帖索引时机。
2. **数据为查询时刻的快照**：`view_count` / `like_count` 等计数取自 ES 文档，可能有秒级同步延迟（经聚合器异步落库）。
3. **`user_id` 恒为本人**：本接口已按当前登录用户过滤，返回项里的 `user_id` 必为本人，可省略二次过滤。
4. **路由前缀**：若网关有全局前缀（如 `/api/v1`），实际路径为 `/api/v1/post/my`，以前端实际部署为准。

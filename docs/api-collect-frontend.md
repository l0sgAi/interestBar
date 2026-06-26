# 帖子收藏 & 互动状态 前端对接文档

> 本文覆盖三类前端可感知的改动：①收藏接口 ②帖子列表的 `collect_count` ③帖子详情的 `is_liked` / `is_collected`。
> 所有接口均需登录。网关若有全局前缀（如 `/api/v1`），实际路径叠加该前缀。

---

## 0. 通用约定

### 鉴权

请求头携带登录 token：

```
satoken: <登录返回的 token>
```

> 请求头名称由配置项 `sa_token.token_name` 决定，当前为 `satoken`。未携带或失效返回 `401`。

### 统一响应包

```jsonc
{
  "code": 200,            // 业务码：200 成功
  "message": "Success",
  "data": { ... }         // 成功时有；失败时无
}
```

错误响应：`{ "code": <code>, "message": "<msg>" }`（无 `data`）。

### 游标分页（`search_after`）

收藏列表采用 keyset 游标分页，**非 offset/page**：

1. 首次请求**不带** `search_after`。
2. 从响应取 `data.search_after`（不透明字符串，**不要解析或修改**）。
3. 取下一页时原样作为 `?search_after=` 传入。
4. 响应 `search_after` 为空字符串 `""` → 已到末页，停止翻页。

---

## 1. 收藏 / 取消收藏

```
POST /collect/toggle
```

幂等切换：同一帖子连续调用会在 收藏↔取消 间切换。

### 请求体

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `post_id` | string(uuid) | 是 | 帖子 ID |

```jsonc
{ "post_id": "0192f8a1-...-..." }
```

### 响应 `data`

| 字段 | 类型 | 说明 |
|---|---|---|
| `is_collected` | bool | **本次切换后**的状态：`true`=已收藏，`false`=已取消 |
| `post_id` | string(uuid) | 帖子 ID |

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "is_collected": true,
    "post_id": "0192f8a1-...-..."
  }
}
```

### 前端要点

- 点击收藏按钮即调用本接口，**用响应里的 `is_collected` 直接更新 UI**（不要本地预判，以服务端为准）。
- 收藏数 `collect_count` 的展示由列表/详情接口返回，**无需前端自己 +1/-1**；切换后若需即时刷新数字，可重新拉取详情或列表（异步聚合，可能有秒级延迟）。

### 错误码

| HTTP | `code` | `message` | 触发场景 |
|---|---|---|---|
| 401 | 202 | `Token not found` | 未登录 |
| 400 | 201 | `Invalid request parameters` | `post_id` 缺失 / 非 UUID |
| 404 | 203 | `Post not found` | 帖子不存在或已删除 |
| 500 | 210 | `Failed to process collect request` | 服务端异常 |

---

## 2. 我的收藏列表

```
GET /collect/posts
```

返回当前登录用户收藏的帖子，按**收藏时间倒序**（最近收藏在前）。

### Query 参数

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `size` | int | 否 | `20` | 每页数量；`<=0` 或 `>100` 回退为 `20` |
| `search_after` | string | 否 | `""` | 上一页响应的 `search_after`，原样透传（见 §0） |

### 响应 `data`

| 字段 | 类型 | 说明 |
|---|---|---|
| `posts` | `PostListItem[]` | 帖子列表（已组装作者/圈子/图片，结构同帖子列表接口） |
| `total` | int | 收藏总数 |
| `size` | int | 本页实际返回条数 |
| `search_after` | string | 下一页游标；空串=末页 |

### `PostListItem` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(uuid) | 帖子 ID |
| `circle_id` | string(uuid) | 所属圈子 ID |
| `user_id` | string(uuid) | 发帖人 ID |
| `type` | int | `1`=图文 / `2`=视频 / `3`=投票 |
| `title` | string | 标题 |
| `summary` | string | 摘要 |
| `content` | string | 正文 |
| `view_count` | int | 浏览量 |
| `comment_count` | int | 评论数 |
| `like_count` | int | 点赞数 |
| `collect_count` | int | **收藏数** |
| `is_pinned` | int | 是否置顶：`0` 否 / `1` 是 |
| `is_essence` | int | 是否精华：`0` 否 / `1` 是 |
| `is_lock` | int | 是否锁定：`0` 否 / `1` 是 |
| `status` | int | 帖子状态（本接口仅返回已发布 `1`） |
| `create_time` | string | 发帖时间（RFC3339Nano） |
| `author_name` | string | 作者昵称 |
| `author_avatar` | string | 作者头像 URL |
| `circle_name` | string | 所属圈子名 |
| `circle_avatar` | string | 所属圈子头像 URL |
| `images` | string[] | 帖子图片 URL 列表 |

### 示例

```bash
# 第一页
curl -G "https://<host>/collect/posts" \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "size=10"

# 第二页
curl -G "https://<host>/collect/posts" \
  -H "satoken: eyJhbGciOi..." \
  --data-urlencode "size=10" \
  --data-urlencode 'search_after=<上一页返回值>'
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
        "summary": "初秋通勤穿搭...",
        "content": "...",
        "view_count": 128,
        "comment_count": 12,
        "like_count": 45,
        "collect_count": 8,
        "is_pinned": 0, "is_essence": 1, "is_lock": 0,
        "status": 1,
        "create_time": "2026-06-20T10:23:45.123456789Z",
        "author_name": "小洛",
        "author_avatar": "https://cdn.../avatar.png",
        "circle_name": "穿搭日记",
        "circle_avatar": "https://cdn.../c.png",
        "images": ["https://cdn.../1.jpg"]
      }
    ],
    "total": 18,
    "size": 1,
    "search_after": "eyJ0Ijoi...}"
  }
}
```

### 前端要点

- **失效帖静默过滤**：若收藏的帖子被作者删除/封禁，该帖不会出现在 `posts` 中（但 `total` 仍含历史计数）。前端无需特殊处理，列表长度可能小于 `size` 属正常。
- 游标翻页过程中不要更换 `size`。

### 错误码

| HTTP | `code` | `message` | 触发场景 |
|---|---|---|---|
| 401 | 202 | `Token not found` | 未登录 |
| 400 | 201 | `Invalid request parameters` | `search_after` 游标非法 |
| 500 | 210 | `Failed to process collect request` | 服务端异常 |

---

## 3. 帖子列表：`collect_count` 字段

下列**已存在**的列表接口，响应 `posts[].collect_count` 字段现已实际被收藏行为驱动（此前为占位 0）：

| 接口 | 说明 |
|---|---|
| `GET /post/list` | 搜索帖子列表 |
| `GET /post/my` | 我的发帖 |
| `GET /post/user/:user_id` | 指定用户发帖 |

前端无需改动请求方式，**只需确保渲染了 `collect_count`**。字段定义见 §2 `PostListItem`。

> 计数取自数据快照，经异步聚合落库，可能有秒级延迟。

---

## 4. 帖子详情：`is_liked` / `is_collected`

```
GET /post/detail/:id
```

详情响应新增 **`is_collected`** 字段（标识当前用户是否已收藏该帖）；`is_liked`（是否已点赞）保持不变。

### 响应 `data` 关键字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string(uuid) | 帖子 ID |
| `circle_id` / `user_id` | string(uuid) | 所属圈子 / 作者 |
| `type` | int | `1`=图文 / `2`=视频 / `3`=投票 |
| `title` / `summary` / `content` | string | 标题 / 摘要 / 正文 |
| `media_extra` | string[] | 媒体（图片 URL 列表） |
| `view_count` | int | 浏览量 |
| `comment_count` | int | 评论数 |
| `like_count` | int | 点赞数 |
| `collect_count` | int | **收藏数** |
| `is_pinned` / `is_essence` / `is_lock` | int | `0` 否 / `1` 是 |
| `status` | int | 帖子状态 |
| `create_time` / `update_time` | string | 时间（RFC3339Nano） |
| `last_reply_time` | string | 最后回复时间（可能省略） |
| `author_id` | string(uuid) | 作者 ID |
| `author_name` / `author_avatar` | string | 作者昵称 / 头像 |
| **`is_liked`** | **bool** | **当前用户是否已点赞** |
| **`is_collected`** | **bool** | **当前用户是否已收藏**（新增） |

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "id": "0192f8a1-...-...",
    "circle_id": "0191ab...-...",
    "user_id": "0190...-...",
    "type": 1,
    "title": "今日穿搭分享",
    "summary": "初秋通勤穿搭...",
    "content": "...",
    "media_extra": ["https://cdn.../1.jpg"],
    "view_count": 128,
    "comment_count": 12,
    "like_count": 45,
    "collect_count": 8,
    "is_pinned": 0, "is_essence": 1, "is_lock": 0,
    "status": 1,
    "create_time": "2026-06-20T10:23:45.123456789Z",
    "update_time": "2026-06-20T10:23:45.123456789Z",
    "author_id": "0190...-...",
    "author_name": "小洛",
    "author_avatar": "https://cdn.../avatar.png",
    "is_liked": true,
    "is_collected": false
  }
}
```

### 前端要点

- 进入详情页：用 `is_liked` / `is_collected` 初始化点赞、收藏按钮状态。
- 点击点赞 → 调 `POST /like/toggle`；点击收藏 → 调 `POST /collect/toggle`。
- 切换后**用各 toggle 接口返回的布尔值更新按钮**（`is_liked` / `is_collected`），数字（`like_count` / `collect_count`）以重新拉取或列表同步为准。

---

## 5. 改动小结（前端 Checklist）

| 改动 | 影响接口 | 前端动作 |
|---|---|---|
| 新增收藏切换 | `POST /collect/toggle` | 接入 |
| 新增我的收藏列表 | `GET /collect/posts` | 接入（注意游标分页） |
| 收藏数生效 | `GET /post/list`、`/post/my`、`/post/user/:id` | 确认渲染 `collect_count` |
| 详情新增收藏状态 | `GET /post/detail/:id` | 读取 `is_collected` 初始化按钮 |
| 详情点赞状态（既有） | `GET /post/detail/:id` | `is_liked` 不变 |

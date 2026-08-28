# @提及精确绑定 — 后端设计文档

> 目标：让前端零搜索精确建链——后端持久化发帖/评论时的最终 @提及 名单，并在帖子详情、
> 评论列表/回复/详情回传 `mentions` 数组；通知名单 == 落库名单。
> 基线：`docs/mention-frontend-api.md`（前端视角需求文档，F1/F2/F3）。
> 状态：**P0 已实现**（本仓库代码已落地；DDL 待 DB-owner 执行）。

## 一、现状盘点（实现前）

- `POST /post/create`、`POST /comment/create` 已接收 `mention_user_ids`：handler 解析
  （`post/interfaces/http/handler.go` `parseMentionUserIDs`）→ application 层
  `filterMentionUserIDs`（去重 → 去自己/Nil → `GetBriefs` 校验存在且未注销 → MentionMax 截断）
  → **仅发 `PublishMentionNotice` 通知事件，从不落库**。post/comment 表无 mention 存储
  ——需求文档"后端已持久化"的前提与代码不符，持久化为本次新增。
- 读路径零回传：`PostDetailVO`、`CommentVO` 无 mentions 字段，前端只能文本反查（搜索排名
  不可靠、访客态不可用、改名死链、冷缓存请求峰值）。
- 已复用的既有能力：
  - "存在且未注销"校验 = `user.app.UserFacade.GetBriefs`（内部过滤
    `Status != UserStatusActive || Deleted != 0`）——F1 写校验与 F2 读过滤天然满足。
  - 装配零改动：`pkg/composition/server.go` 的 `newPostService`/`newCommentService`
    已构造 repo 注入，userFacade 桥接已存在（`facade_bridges.go`）。

## 二、方案与数据流

### 存储选型

按域拆分的关系表（post/comment 各管各的，不共享 content_type 表避免跨域写他人表；
不用 JSON 列，因为批量 IN 读、唯一约束、将来"提及我的"反查都需要关系表）。
append-only、不设 deleted：提及行随内容生灭。

数据流：

```
写：CreatePost/CreateComment
  mention_user_ids (handler parseMentionUserIDs)
    → filterMentionUserIDs 一次校验截断（GetBriefs 过滤未注销）
    → repo.CreateMentions 落库（append-only，ON CONFLICT DO NOTHING 幂等）
    → 同一份名单 PublishMentionNotice（仅已发布帖；评论无状态门槛）
    [落库/通知均 best-effort error log，不阻断创建]

读：GET /post/detail        → GetMentionUserIDsByPostIDs([postID])  → GetBriefs → vo.Mentions
    GET /comment/list       → GetMentionUserIDsByCommentIDs(本页IDs) → 并入作者/被回复人
    GET /comment/replies       一次 GetBriefs                        → items[*].mentions
    GET /comment/detail/:id → 单条查 + GetBriefs → vo.Mentions
```

### Schema 变更

DDL 权威来源（**待 DB-owner 执行**）：`docs/pgsql-ddl/post.md`（`domains.post_mention`）、
`docs/pgsql-ddl/comment.md`（`domains.comment_mention`）。两表同构：UUIDv7 主键 + 内容ID +
user_id + create_time；UNIQUE(内容ID, user_id)（幂等写 + 防重）；
idx(user_id, create_time DESC)（预留"提及我的"反查）。无 deleted 列。

### 契约（响应新增字段）

```json
"mentions": [{ "id": "uuid", "username": "当前用户名", "avatar_url": "..." }]
```

| 接口 | 位置 | 组装点 |
|---|---|---|
| `GET /post/detail` | 帖子对象 | `assembleMentions` (service.go:600) → `PostDetailVO.Mentions` (:193) |
| `GET /comment/list` | `items[*]` | `buildCommentVOs` (:451) 经 `buildMentionVOs` (:556) |
| `GET /comment/replies` | `items[*]` | 同上（同走 buildCommentVOs） |
| `GET /comment/detail/:id` | 评论对象 | GetCommentDetail (:407) 单条组装（需求未列，同构顺带补齐） |

- 空名单返回 `[]`（非 null）：前端按"缺失/空数组回退文本反查"处理。
- 仅返回未注销用户（GetBriefs 已过滤）；按提及写入顺序（UUIDv7 `id ASC` ≈ 正文出现顺序）。
- 帖子列表/卡片接口不回传（卡片不渲染正文，避免响应体放大）。
- 创建接口不回传 mentions（需求标注"非必须"，保持返回纯 ID）。

## 三、写入语义（F1）

- `filterMentionUserIDs`（post:426 / comment:624）语义不变：去重 → 去自己/Nil →
  GetBriefs（存在且未注销，查询失败整体降级为空名单）→ MentionMax（`notice.mention_max`，
  默认 10）截断，重复提及不占配额。
- 落库与通知用**同一份 filter 结果**：通知名单 == 落库名单（F3）。
  - post（service.go:429）：落库不区分帖子状态（草稿正文同样含提及）；**通知仅已发布**
    （保持既有策略，草稿/审核中不对外产生通知）。
  - comment（service.go:301）：落库后经 `publishCommentNotifications` 发 mention 通知
    （该函数签名从"收原始列表自行 filter"改为"收已校验落库的最终名单"）。
- 请求里不传 → filter 返回空 → 不落库、不通知（F1 收尾条款）。

## 四、一致性/边界/风险

| 边界 | 行为 |
|---|---|
| 用户改名 | mentions 回传**当前** username，正文 token 是发帖时旧名 → 前端整名匹配 miss（不建链、不会错链）。不存历史名快照，契约内边界（需求 §4"数组即权威映射"） |
| 被提及者事后注销 | 落库名单不变；读路径经 GetBriefs 过滤后不出现在 mentions（满足"仅返回未注销"） |
| user 服务不可用 | filter 整体降级空名单（既有行为）→ 不落库不通知，等同现状 |
| 落库/通知失败 | 各自 error log，不阻断创建；前端回退文本反查（mentions 缺失合规） |
| 重复提交同一名单 | ON CONFLICT DO NOTHING 幂等，唯一索引 (内容ID, user_id) 兜底 |
| 读放大 | 每详情/每页一次 IN 查询（≤20 行），GetBriefs 走 userinfo 缓存，与现有组装同模式 |

## 五、验证

`go build ./... && go vet ./... && go test ./pkg/...` 全绿。
DB-owner 执行新 DDL 后，curl 全流程：A 带 `mention_user_ids:[B]` 发帖 → detail 回传
mentions 且通知仅 B；重复/自己/不存在/超 10 个 → 去重截断；不传 → `mentions: []` 无通知；
评论 + 楼层回复带提及 → list/replies/detail 各自回传；把 B 置 `status != 1` 模拟注销 →
mentions 中 B 消失、无报错。

# 帖子收藏功能设计文档（待 Review）

> 状态:**设计待确认**,未开工。本文沿用 [点赞系统](../pkg/domains/like) 的架构范式,给出收藏功能的库表、接口、缓存、消息链路设计。
> 库表 DDL 见 [`db.md` → 帖子收藏表](./db.md#帖子收藏表)。

---

## 1. 概述

「收藏」= 用户对帖子的二元状态(收藏/取消),语义上独立于「点赞」:点赞表达认同,收藏表达「留待后看」。本功能提供:

1. **收藏/取消收藏(幂等 Toggle)** —— 帖子详情页、信息流卡片上的收藏按钮。
2. **我的收藏列表** —— 个人中心「我的收藏」页,可回看已收藏帖子。← 与点赞相比的新增能力(点赞无此列表接口)。
3. **信息流「是否已收藏」回显** —— 批量判定当前用户对一组帖子是否已收藏。

### 与点赞系统的差异

| 维度 | 点赞(已实现) | 收藏(本文) |
|---|---|---|
| 目标范围 | post + comment(两种目标,`type` 区分) | **仅 post**(评论无收藏语义) |
| 列表接口 | 无 | **有** `GET /collect/posts` |
| 统计字段 | `post.like_count` / `comment.like_count` | `post.collect_count`(表/Hash/消息链路均已预留) |
| 领域归属 | 独立 `like` 领域 | **独立 `collect` 领域**(推荐,见 §3.1) |

---

## 2. 数据模型

### 2.1 流水表 `domains.post_collect`

DDL 已写入 [`db.md`](./db.md#帖子收藏表)。与 `post_like` 字段一一对应:

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID | 主键(UUIDv7,应用层 `sharedomain.NewID()` 生成) |
| `user_id` | UUID | 收藏人 |
| `post_id` | UUID | 帖子 |
| `deleted` | SMALLINT | `0`=有效 / `1`=取消 |
| `create_time` / `update_time` | TIMESTAMPTZ | 时间 |

索引三件套(沿用点赞表命名与覆盖索引风格):

- `uk_post_collect_user_post` UNIQUE `(user_id, post_id)` —— 幂等保证
- `idx_pcollect_user_active` `(user_id, create_time DESC, id DESC) WHERE deleted=0` —— **我的收藏列表**(支持 keyset 游标)
- `idx_pcollect_post_active` `(post_id, create_time DESC) WHERE deleted=0` —— 收藏者列表

> ⚠️ PK 生成注意:沿用项目既有约定——DDL 的 `DEFAULT uuidv7()` 仅兜底,**应用层 GORM `BeforeCreate` 钩子 / 显式 `sharedomain.NewID()` 必须调用**,否则零值 UUID 与默认值冲突。参考记忆 [[uuidv7-primary-key-generation]]。

### 2.2 统计字段(已就绪,无需改动)

- DB:`domains.post.collect_count`(已存在)
- Redis:`post:stats:{post_id}` Hash 的 `collect_count` 字段(已存在,与 `like_count` 同 Hash)
- MQ:`StatisticsTypePostCollect = "post_collect_count"` + `PostStatisticsAggregator` 已处理该 delta + `PublishPostCollectCount()` 生产者已存在

---

## 3. 架构设计(Review 重点)

### 3.1 领域划分:新建独立 `collect` 领域 ✅ 推荐

```
pkg/domains/collect/
├── application/
│   └── service.go          # CollectService: Toggle + ListCollectedPosts
├── domain/
│   ├── collect.go          # PostCollect 实体 + 常量 + 哨兵错误
│   └── repository.go       # PostCollectCache / CollectEventPublisher / PostTarget 端口
├── infrastructure/
│   ├── collect_cache_redis.go      # 复用 redis 包的 collect Lua 脚本
│   └── collect_event_publisher.go  # Redpanda 生产者
└── interfaces/http/
    ├── handler.go          # ToggleCollect / ListCollectedPosts
    └── routes.go           # POST /collect/toggle, GET /collect/posts
```

**为什么独立成域(而非塞进 `like` 领域)**:

- 代码库已有「每业务概念一个 domain 包」的强约定(post/comment/like/circle/user 各自独立),collect 同等地位。
- 收藏独有「列表回看」用例,塞进 like 会让 `LikeService` 承担不属于它的查询职责,命名与演进耦合。
- 未来若加「收藏夹/分组」(单帖子可入多个夹),collect 独立才能干净扩展。

**被否决方案**:扩展现有 `like` 领域,把 collect 当第三种 `type`。优点是少写一个包,代价是领域语义混淆(「点赞服务」管收藏)、`type` 枚举膨胀。不推荐。

### 3.2 Toggle 流程(镜像点赞,4 步)

完全复刻 [`togglePostLike`](../pkg/domains/like/application/service.go):

```
1. 校验帖子存在        → postTarget.Exists(postID)         [复用 post 领域端口]
2. 恢复统计缓存        → postTarget.RestoreStats(postID)   [复用,确保 stats Hash 存在]
3. Lua 原子切换        → postCollectCache.Toggle(user,post) [ZSET 增删 + collect_count 增减]
4. 发布收藏事件        → publisher.PublishPostCollect(...)  [异步落库]
```

跨领域依赖(`PostTarget` 接口)由 composition 层 setter 注入,与 like 完全一致。

### 3.3 Redis 设计

| 用途 | Key | 类型 | 说明 |
|---|---|---|---|
| 用户收藏帖子集合 | `user:collect:posts:{user_id}` | ZSET | member=post_id, score=收藏时间戳;**镜像** `user:like:posts:{user_id}` |
| 帖子统计(复用) | `post:stats:{post_id}` | Hash | 字段 `collect_count`,已存在 |

**Lua 原子脚本**——与 [`likeToggleScript`](../pkg/server/storage/redis/like_lua.go) 逻辑完全相同,仅 `HINCRBY` 的 Hash 字段从 `'like_count'` 换成 `'collect_count'`。两种落地方式:

- **方案 A(推荐):参数化复用**。给现有脚本增加 `ARGV[5]=field_name`,Toggle 时传 `'like_count'` 或 `'collect_count'`。一处脚本服务两种动作,DRY。风险:动了已被 like 依赖的热代码,需回归点赞。
- **方案 B(保守):复制脚本**。新增 `collectToggleScript` + `collectToggleSHA`,与 like 脚本独立。零回归风险,代价是两段几乎相同的 Lua。

> Review 取舍:A 省 maintainer 成本但需回归点赞;B 最稳。**默认推荐 B**(收藏是新功能,点赞已稳定,不动它),若团队偏好 DRY 则选 A。

**批量判定(信息流回显)**:镜像 `BatchCheckPostLiked` / `BackfillPostLikes`,新增 `BatchCheckPostCollected` / `BackfillPostCollects`,读 `user:collect:posts:{user_id}` ZSET。ZSET 只缓存最近 2000 条(脚本 `maxSize` 上限),超限回源 DB `post_collect WHERE deleted=0`。

### 3.4 消息与异步落库(关键设计决策)

`post.collect_count` 统计已有两条可用路径,需二选一:

- **路径 ①(推荐):镜像点赞,单 topic 双写**。新增 collect 事件 topic(或复用 `LikeEventTopic` 加 `type=post_collect`)。消费者 `batchUpdatePostCollects` 在**同一事务**内:(a) upsert `post_collect` 流水行;(b) 批量 `UPDATE post SET collect_count += delta`。与 [`batchUpdatePostLikes`](../pkg/server/storage/redpanda/like_consumer.go) 逐行对齐。优点:流水与统计原子一致、与点赞范式统一、`post_collect` 行是「我的收藏列表」的唯一权威数据源。
- **路径 ②:复用既有 PostTopic 统计链路**。Toggle 时调现成的 `PublishPostCollectCount(postID, ±1)` → PostTopic → `PostStatisticsAggregator` 落 `collect_count`;另起一个 topic 单独持久化 `post_collect` 流水行。优点:统计链路零开发。缺点:**一次收藏发两个 topic、两个消费者**,流水与统计可能短暂不一致,且 `post_collect` 流水仍需新建消费者——并没省事。

> **推荐路径 ①**。路径 ② 把现成的 `PublishPostCollectCount` / `StatisticsTypePostCollect` / Aggregator 的 collect 分支变成冗余(可保留作 fallback 或清理)。

### 3.5 「我的收藏」列表 `GET /collect/posts`

点赞没有列表接口,收藏需要。设计:

- **数据源**:DB `post_collect`(`deleted=0`),按 `create_time DESC` keyset 分页。ZSET 仅用于信息流「是否已收藏」回显,**不**作为列表权威源(有 2000 条上限 + TTL 失活)。
- **组装**:取出 `post_id` 列表后,复用 post 领域现有的帖子组装逻辑(作者/圈子/图片,产出 `PostListItem`,与 [`/post/my`](./api-post-my.md) 同结构)。
- **失效帖处理**:帖子被作者删除/封禁后,收藏行保留(流水不动),列表查询时 INNER JOIN `post WHERE deleted=0 AND status=1` 过滤掉失效帖。是否对用户可见「收藏已失效」提示,留作产品决策。

---

## 4. 接口规范

### 4.1 收藏/取消收藏 `POST /collect/toggle`

鉴权:需登录(`satoken`,同全站)。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `post_id` | string(uuid) | 是 | 帖子 ID |

```jsonc
{ "post_id": "0192f8a1-...-..." }
```

**响应**

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "is_collected": true,            // true=已收藏(本次切换后), false=已取消
    "post_id": "0192f8a1-...-..."
  }
}
```

幂等:对同一 `post_id` 连续调用会在 收藏↔取消 间切换,与点赞 Toggle 行为一致。

### 4.2 我的收藏列表 `GET /collect/posts`

鉴权:需登录。Query:

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `size` | int | 否 | `20` | 每页数量,`<=0` 或 `>100` 回退 `20` |
| `search_after` | string | 否 | `""` | 上一页返回的游标,原样透传 |

**响应**(结构同 [`/post/my`](./api-post-my.md) 的 `PostSearchResult`)

```jsonc
{
  "code": 200,
  "message": "Success",
  "data": {
    "posts": [ /* PostListItem[] */ ],
    "total": 18,
    "size": 10,
    "search_after": "eyJ0Ijoi..."}"   // 空=末页
  }
}
```

> `search_after` 为不透明游标(内部编码 `create_time`+`id`),客户端不要解析。排序:收藏时间倒序(最近收藏在前)。

### 4.3 错误码

| HTTP | `code` | `message` | 触发场景 |
|---|---|---|---|
| 401 | 202 | `Token not found` | 未登录 |
| 400 | 201 | `Invalid request parameters` | `post_id` 缺失/非 UUID |
| 404 | 203 | `Post not found` | 帖子不存在/已删除 |
| 500 | 210 | `Failed to toggle collect` | 缓存/Lua/消息异常 |

---

## 5. 实施清单(确认设计后执行)

1. **DB**:执行 `domains.post_collect` DDL(已写入 db.md)。
2. **Redis**:`pkg/server/storage/redis/` 新增 collect Lua 脚本(方案 A 或 B)+ `collect_lua.go`(`TogglePostCollect` / `BatchCheckPostCollected` / `BackfillPostCollects`)+ 常量 `UserPostCollectListPrefix = "user:collect:posts:"`。
3. **领域**:`pkg/domains/collect/` 四层脚手架(application/domain/infrastructure/interfaces)。
4. **MQ**:collect 事件生产者 + 消费者 `batchUpdatePostCollects`(流水 upsert + collect_count 批量 UPDATE,同一事务)。
5. **装配**:`pkg/composition/server.go` 新增 `newCollectService` + setter 注入 `PostTarget`;`InitLikeLuaScripts` 旁加 `InitCollectLuaScripts` 启动预加载。
6. **路由**:`POST /collect/toggle`、`GET /collect/posts` 挂到 `/collect` 组(带 authCheck)。

---

## 6. 待 Review 确认项

1. **领域划分**:同意新建独立 `collect` 领域?(还是坚持塞进 `like`)
2. **Lua 脚本**:方案 A(参数化复用)vs 方案 B(复制独立)?默认推荐 B。
3. **落库路径**:路径 ①(单 topic 双写,镜像点赞)vs 路径 ②(复用 PostTopic 统计)?默认推荐 ①。
4. **失效帖**:收藏的帖子被删/封禁后,列表是静默过滤,还是给「已失效」提示?
5. **评论收藏**:当前明确不做(评论无收藏语义),确认?
6. **收藏夹/分组**:本期不做,但表结构是否需要预留 `folder_id` 可空字段?(不预留则未来加收藏夹需改表;预留则当前多一冗余列。默认不预留,未来按需加表。)

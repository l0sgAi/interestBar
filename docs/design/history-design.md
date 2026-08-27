# 用户浏览历史（post_view_history）设计与实现方案 v2

> 目标：用户查看帖子详情时**自动记录浏览**；提供「最近浏览」列表接口，列表数据**走 ES** 查询。
>
> 架构：**Redis ZSET 即时读**（按访问时间排序 + 帖子去重，cap 500）+ **Redpanda MQ 异步落库**（复用 like/collect 事件链路）+ **DB 持久化**（冷启动回源 + 审计）。
>
> 参照对象：`like` / `collect` 领域。两者同构——Redis ZSET（Lua 原子）+ MQ 事件（producer → aggregator → consumer 批量 upsert）。history 是「无 toggle 的单向记录」版。

---

## 一、架构总览（数据流）

```
                         ┌─────────────────────────────────────────────────────┐
   GET /post/detail      │  post.application.GetPostDetail                     │
   ───────────────────▶  │    └ go asyncIncrementView(postID, userID)          │
                         │         ├─ IncrementPostViewCount (已有, 5min 去重) │
                         │         │      └─ newCount>0 ? 真实浏览 : 跳过      │
                         │         └─ ★ historyRecorder.RecordView(userID,postID)  ← 新增钩子
                         └──────────────┬──────────────────────────────────────┘
                                        │ (history 域,跨领域端口注入)
                                        ▼
                         ┌─────────────────────────────────────────────────────┐
                         │  history.application.RecordView                     │
                         │    1. Redis ZSET  ZADD user:view:posts:{uid}        │
                         │         score=now(ms) member=postID + trim 500      │ ← Lua 原子(复用 collect 脚本结构)
                         │    2. PublishPostViewHistoryEvent(userID, postID)   │ ← MQ 异步
                         └──────────────┬──────────────────────────────────────┘
                                        │ Redpanda topic: post_view_history
                                        ▼
                         ┌─────────────────────────────────────────────────────┐
                         │  HistoryEventAggregator (内存聚合,复用 like 模式)    │
                         │    deltas["uid:pid"] += 1                           │
                         │    ticker flush (N 秒) → batch upsert               │
                         └──────────────┬──────────────────────────────────────┘
                                        ▼
                         ┌─────────────────────────────────────────────────────┐
                         │  post_view_history 表  UPSERT                        │
                         │    ON CONFLICT (user_id,post_id) DO UPDATE           │
                         │      update_time=NOW(), view_count=view_count+1     │
                         └─────────────────────────────────────────────────────┘

   GET /history/posts
   ───────────────────▶  history.application.ListHistoryPosts
                           1. Redis ZREVRANGE user:view:posts:{uid} offset,size  ← 即时读
                              (ZCARD==0 冷启动 → DB top500 回源 + Backfill ZSET)
                           2. postFetcher.SearchByIDs(postIDs)  ← ES terms 查询
                           3. 按 ZSET 顺序重排 + 组装(复用 post.assemblePostList)
                           ▶ 返回 PostListItem[] + total + next_offset
```

---

## 二、关键决策

| 决策点 | 选型 | 理由 |
|---|---|---|
| 读路径 | **Redis ZSET 即时读**（`ZREVRANGE` offset 分页） | 遵用户要求；ZSET score=访问时间天然倒序；cap 500 无深翻页问题 |
| 写路径 Redis | **Lua 原子**（ZADD + ZREMRANGEBYRANK trim 500 + EXPIRE） | 复用 `collectToggleScript` 结构；1 RTT；与现有 ZSET 操作一致 |
| 写路径 DB | **MQ 异步聚合落库**（复用 like/collect consumer） | 高频浏览不能同步写 DB；批量 upsert 降压 |
| 去重粒度 | **复用 `asyncIncrementView` 的 5min 去重**（`newCount>0` 才记 history） | 天然防刷新刷量；无需额外去重 key；语义=「真实浏览才入历史」 |
| ZSET cap | **500**（用户要求） | 超出按最低 score 淘汰（最久未访）；`ZREMRANGEBYRANK key 0 n-1` |
| ZSET TTL | **复用 `postStatsTTL`**（与 collect ZSET 一致） | 冷数据自动过期；DB 仍是权威持久层 |
| 冷启动回源 | **ZCARD==0 时 DB top500 + Backfill ZSET** | ZSET 过期后返回用户仍可见历史；对称 collect 的 Backfill |
| 记录模型 | **去重 upsert**（每对 user+post 一行，bump update_time + view_count） | 「最近浏览」语义；表体量可控 |
| 帖子组装数据源 | **ES**（新增 `SearchPostsByIDs`） | 遵用户要求；与帖子列表搜索同源；复用 `assemblePostList` |
| 归属领域 | **新建 `history` 独立领域** | 对齐 DDD 分域；与 like/collect 同构 |
| 软删 `deleted` | **不加** | history 无 toggle 语义；「清空历史」=硬 DELETE |

---

## 三、数据表设计

> 遵循 [docs/db.md](./db.md) 规范，与 `post_collect` 对齐。差异：无 `deleted`、多 `view_count`、排序键 `update_time`。

```sql
-- 帖子浏览历史表 (post_view_history)
DROP TABLE IF EXISTS domains.post_view_history;

CREATE TABLE domains.post_view_history (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL,           -- 浏览人ID
    post_id UUID NOT NULL,           -- 被浏览的帖子ID
    view_count INT NOT NULL DEFAULT 1, -- 该帖被该用户浏览次数
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, -- 首次浏览
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP  -- 最近浏览(排序键)
);

COMMENT ON TABLE domains.post_view_history IS '帖子浏览历史表(去重模型:每对 user+post 一行,再看时 bump update_time+view_count)';
COMMENT ON COLUMN domains.post_view_history.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';
COMMENT ON COLUMN domains.post_view_history.user_id IS '浏览人ID(UUID)';
COMMENT ON COLUMN domains.post_view_history.post_id IS '被浏览的帖子ID(UUID)';
COMMENT ON COLUMN domains.post_view_history.view_count IS '该帖被该用户浏览次数(MQ 聚合 +1)';
COMMENT ON COLUMN domains.post_view_history.update_time IS '最近浏览时间(排序键,冷启动回源用)';

-- 去重约束:支撑 MQ consumer 的 ON CONFLICT upsert
CREATE UNIQUE INDEX uk_pviewhist_user_post ON domains.post_view_history(user_id, post_id);
-- 冷启动回源:按 update_time 倒序取 top500 回填 ZSET
CREATE INDEX idx_pviewhist_user_time ON domains.post_view_history(user_id, update_time DESC, id DESC);
```

---

## 四、Redis ZSET 设计

### 4.1 key 命名（`pkg/server/storage/redis/constants.go` 新增）

```go
// UserPostViewListPrefix 用户浏览历史 ZSET key 前缀
// 完整 key: user:view:posts:{user_id}
// Score: 最后访问时间戳(Unix 毫秒), Member: postId(UUID 字符串)
// 与 user:collect:posts / user:like:posts 同构。
const UserPostViewListPrefix = "user:view:posts:"

// GetUserPostViewListKey 获取用户浏览历史 ZSET 的完整 key
func GetUserPostViewListKey(userID uuid.UUID) string {
    return UserPostViewListPrefix + userID.String()
}
```

### 4.2 Lua 脚本（`pkg/server/storage/redis/history_lua.go` 新增，复用 collect 脚本结构）

```go
// historyRecordScript 浏览记录原子脚本:ZADD(upsert score) + 超限 trim + 续期。
// 与 collectToggleScript 同构,但无 toggle/ZREM/stats Hash 交互(history 无反向操作)。
const historyRecordScript = `
local zsetKey  = KEYS[1]
local postId   = ARGV[1]
local now      = tonumber(ARGV[2])
local maxSize  = tonumber(ARGV[3])
local ttl      = tonumber(ARGV[4])

-- upsert:已存在则更新 score(移到顶部),不存在则插入
redis.call('ZADD', zsetKey, now, postId)

-- trim:仅保留最近 maxSize 条(按 score 降序),淘汰最低分
local size = redis.call('ZCARD', zsetKey)
if tonumber(size) > maxSize then
    local removeCount = tonumber(size) - maxSize
    redis.call('ZREMRANGEBYRANK', zsetKey, 0, removeCount - 1)
end

redis.call('EXPIRE', zsetKey, ttl)
return 1
`

// RecordPostView 原子记录浏览(ZADD + trim 500 + EXPIRE)。maxSize=500,TTL=postStatsTTL。
func RecordPostView(userID, postID uuid.UUID) error { /* EvalSha + 失败重载,同 collect */ }

// ListPostViews ZREVRANGE 倒序取 [offset, offset+size-1] 的 postID。
func ListPostViews(userID uuid.UUID, offset, size int) ([]string, int64, error) { /* ZREVRANGEByIndex + ZCARD */ }

// BackfillPostViews DB 回源后批量 ZADD 回填(同 BackfillPostCollects)。
func BackfillPostViews(userID uuid.UUID, postIDs []uuid.UUID) error { /* pipeline ZAdd + Expire */ }

// InitHistoryLuaScripts 启动时预加载(同 InitCollectLuaScripts)。
func InitHistoryLuaScripts() error { /* ScriptLoad */ }
```

> `postStatsTTL` 已存在（collect/like 共用），history ZSET 复用同一 TTL。

---

## 五、MQ 异步落库（复用 like/collect 链路）

### 5.1 配置（`pkg/conf/conf.go` Redpanda struct 末尾新增）

```go
HistoryEventTopic         string `mapstructure:"history_event_topic" json:"history_event_topic" yaml:"history_event_topic"`
HistoryEventConsumerGroup string `mapstructure:"history_event_consumer_group" json:"history_event_consumer_group" yaml:"history_event_consumer_group"`
HistoryEventFlushInterval int    `mapstructure:"history_event_flush_interval" json:"history_event_flush_interval" yaml:"history_event_flush_interval"`
```

`configs/config.yaml` 同步新增（topic 名 `post_view_history`，flush 单位秒，对齐 collect）。

### 5.2 Producer（`pkg/server/storage/redpanda/producer.go` 新增，照搬 collect）

```go
var historyEventWriter *kafka.Writer  // 加入顶部 var 块

// InitHistoryEventProducer 初始化浏览历史事件 Producer(照搬 InitCollectEventProducer)。
func InitHistoryEventProducer() error { /* kafka.Writer{Topic: HistoryEventTopic, ...} */ }

// HistoryEventMessage 浏览历史事件消息。
type HistoryEventMessage struct {
    Type   string    `json:"type"`    // 固定 "post_view"
    UserID uuid.UUID `json:"user_id"`
    PostID uuid.UUID `json:"post_id"`
}
const HistoryEventType = "post_view"

// PublishPostViewHistoryEvent 发布浏览历史事件。
func PublishPostViewHistoryEvent(userID, postID uuid.UUID) error {
    // marshal HistoryEventMessage → WriteMessages (key=userID:postID)
}

// CloseHistoryEventProducer 关闭(照搬 CloseCollectEventProducer)。
```

### 5.3 Consumer（`pkg/server/storage/redpanda/history_consumer.go` 新增，照搬 collect_consumer.go）

```go
// HistoryEventAggregator 浏览历史事件聚合器(同构 CollectEventAggregator)。
//   deltas key = "userID:postID" → 去重(同 user+post 多次浏览在 flush 窗口内合并为 1 次 upsert)。
type HistoryEventAggregator struct {
    mu sync.Mutex
    deltas map[string]*historyEventDelta  // {UserID, PostID, Count}
    ticker *time.Ticker
    ...
}

// StartHistoryEventConsumer / StartHistoryEventConsumerWithRetry:照搬 collect。
//   topic=HistoryEventTopic, group=HistoryEventConsumerGroup, flush=HistoryEventFlushInterval 秒。

// flush → batchUpdatePostViewHistory(valid deltas):
func batchUpdatePostViewHistory(deltas []*historyEventDelta) error {
    return pgsql.DB.Transaction(func(tx *gorm.DB) error {
        // 单条 upsert 语义(复用 collect_consumer.batchUpdatePostCollects 的 upsert 模式):
        //   行存在 → UPDATE update_time=NOW(), view_count=view_count+1
        //   行不存在 → CREATE (PK = sharedomain.NewID())
        //   并发 duplicate key 吞掉(幂等)
        // 注:也可用 ON CONFLICT 单 SQL 批量(见 §三),二选一,推荐 ON CONFLICT 更简洁。
    })
}
```

**推荐用 `ON CONFLICT` 批量 SQL**（比 collect 的逐行 upsert 更高效）：

```sql
-- 单事务内逐条 ON CONFLICT upsert(或攒成 jsonb_to_recordset 批量)
INSERT INTO domains.post_view_history (id, user_id, post_id, view_count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (user_id, post_id) DO UPDATE
SET update_time = CURRENT_TIMESTAMP,
    view_count  = post_view_history.view_count + 1;
```

---

## 六、领域结构

```
pkg/domains/history/
├── domain/
│   ├── history.go          # PostViewHistory 实体 + TableName + ErrInvalidCursor
│   ├── cache.go            # PostHistoryCache 接口(RecordView/ListViews/Exists/Backfill)
│   ├── repository.go       # PostHistoryRepository 接口(BatchUpsert/ListTopByUserID)
│   └── publisher.go        # HistoryEventPublisher 接口(PublishPostView)
├── application/
│   └── service.go          # HistoryService + PostFetcher(ES) + DTO
├── infrastructure/
│   ├── history_cache_redis.go        # PostHistoryCache impl(调 redis.RecordPostView 等)
│   ├── history_repository.go         # GORM impl
│   └── history_event_publisher.go    # Redpanda impl
└── interfaces/http/
    ├── handler.go          # GET /history/posts
    └── routes.go
```

### 6.1 接口定义

```go
// domain/cache.go
type PostHistoryCache interface {
    // RecordView ZADD + trim 500(原子 Lua)。
    RecordView(ctx context.Context, userID, postID uuid.UUID) error
    // ListViews 倒序取 [offset, offset+size),返回 postIDs + 总数(ZCARD)。
    ListViews(ctx context.Context, userID uuid.UUID, offset, size int) (postIDs []uuid.UUID, total int64, err error)
    // Exists ZSET 是否存在(用于冷启动判断)。
    Exists(ctx context.Context, userID uuid.UUID) (bool, error)
    // Backfill DB 回源后批量回填 ZSET。
    Backfill(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) error
}

// domain/repository.go
type PostHistoryRepository interface {
    // BatchUpsertView MQ consumer 调用:批量 upsert post_view_history。
    BatchUpsertView(ctx context.Context, deltas []ViewDelta) error
    // ListTopByUserID 冷启动回源:按 update_time 倒序取 top size 条 postID。
    ListTopByUserID(ctx context.Context, userID uuid.UUID, size int) ([]uuid.UUID, error)
}

// domain/publisher.go
type HistoryEventPublisher interface {
    PublishPostView(ctx context.Context, userID, postID uuid.UUID) error
}

// application/service.go
type HistoryService interface {
    // RecordView 浏览记录(供 post 域 async 回调):Redis ZSET + MQ 事件。
    // 失败仅记日志,不影响详情接口。
    RecordView(ctx context.Context, userID, postID uuid.UUID) error
    // ListHistoryPosts 「最近浏览」列表(Redis 即时读 + ES 组装)。
    ListHistoryPosts(ctx context.Context, userID uuid.UUID, size, offset int) (*ListHistoryPostsResult, error)
    SetPostFetcher(f PostFetcher)
}

// PostFetcher history 领域需要的帖子组装能力(ES 来源)。
type PostFetcher interface {
    SearchByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error)
}
```

### 6.2 写入实现（RecordView，对称 collect.Toggle）

```go
func (s *historyServiceImpl) RecordView(ctx, userID, postID) error {
    // 1. Redis ZSET 即时写(ZADD + trim 500) — 列表权威源(即时可见)
    if err := s.cache.RecordView(ctx, userID, postID); err != nil {
        return fmt.Errorf("failed to record view in redis: %w", err)
    }
    // 2. 发布 MQ 事件(异步落库 DB) — 失败仅日志(DB 最终一致由后续浏览补偿)
    if err := s.publisher.PublishPostView(ctx, userID, postID); err != nil {
        logger.Log.Error("Failed to publish post view event: " + err.Error())
    }
    return nil
}
```

### 6.3 查询实现（ListHistoryPosts，Redis 即时读 + ES）

```go
func (s *historyServiceImpl) ListHistoryPosts(ctx, userID, size, offset) (*ListHistoryPostsResult, error) {
    if size <= 0 || size > 100 { size = 20 }

    postIDs, total, err := s.cache.ListViews(ctx, userID, offset, size)
    if err != nil { return nil, err }

    // 冷启动:ZSET 空 → DB top500 回源 + Backfill
    if total == 0 {
        if ids, e := s.repo.ListTopByUserID(ctx, userID, 500); e == nil && len(ids) > 0 {
            _ = s.cache.Backfill(ctx, userID, ids)
            postIDs, total, _ = s.cache.ListViews(ctx, userID, offset, size)
        }
    }

    posts := make([]postapp.PostListItem, 0, len(postIDs))
    if len(postIDs) > 0 {
        fetched, err := s.postFetcher.SearchByIDs(ctx, postIDs)  // ← ES terms 查询
        if err != nil { return nil, err }
        posts = orderByViewTime(fetched, postIDs)  // 按 postIDs(ZSET 倒序)重排,复用 collect 模式
    }
    return &ListHistoryPostsResult{Posts: posts, Total: total, Size: len(posts), NextOffset: nextOffset(offset, size, total)}, nil
}
```

> **分页说明**：ZSET 用 offset 分页（`ZREVRANGE offset offset+size-1`），非 keyset。因 cap 500，无深翻页性能问题。响应含 `next_offset`（末页为 -1 / 不返回）。这与 collect 的 `search_after`（DB keyset）不同——数据源不同（Redis ZSET vs DB），分页机制合理差异。

---

## 七、HTTP 接口

| 方法 | 路径 | 说明 | 入参 | 鉴权 |
|---|---|---|---|---|
| GET | `/history/posts` | 「最近浏览」列表（最近访问时间倒序，ZSET offset 分页） | `size`、`offset`(query，默认 0) | 需登录 |

响应：

```json
{
  "posts": [ /* PostListItem[]，同帖子列表搜索返回结构 */ ],
  "total": 123,
  "size": 20,
  "next_offset": 20
}
```

handler / routes 直接照搬 [collect/interfaces/http](../pkg/domains/collect/interfaces/http)（`requireUserID`、`writeHistoryError`、`RegisterRoutes` 挂 `/history` 组 + `authCheck`）。

---

## 八、ES by IDs 能力（post 共享，复用 assemblePostList）

与 v1 相同，无变更：

- `pkg/server/storage/elasticsearch/post.go`：新增 `SearchPostsByIDs(postIDs []string, size int)`（`terms` 查询 + deleted=0/status=1 过滤，`parsePostSearchResponse` 复用）。
- `pkg/domains/post/application/service.go`：`PostSearcher` 接口加 `SearchByIDs`；`PostService` 接口加 `SearchPostsByIDs`；impl 复用 `assemblePostList`（ES→PostListItem 完整组装 + Redis stats overlay）。
- `pkg/domains/post/infrastructure/post_searcher_es.go`：加 `SearchByIDs` impl。

---

## 九、写入钩子（post 域 async 回调）

### 9.1 post 域端口（`pkg/domains/post/domain/history_port.go` 新增）

```go
// HistoryRecorder 浏览历史记录端口(供 post 域详情页 async 回调,composition 注入 history 实现)。
type HistoryRecorder interface {
    RecordView(ctx context.Context, userID, postID uuid.UUID) error
}
```

### 9.2 post service 变更（`pkg/domains/post/application/service.go`）

```go
// PostService 接口新增 setter:
SetHistoryRecorder(r domain.HistoryRecorder)

// postServiceImpl 新增字段:
historyRecorder domain.HistoryRecorder

// SetHistoryRecorder impl
func (s *postServiceImpl) SetHistoryRecorder(r domain.HistoryRecorder) { s.historyRecorder = r }

// asyncIncrementView 末尾追加(IncrViewCount 返回 newCount>0 即真实浏览,天然 5min 去重):
if newCount > 0 && s.historyRecorder != nil {
    if err := s.historyRecorder.RecordView(context.Background(), userID, postID); err != nil {
        logger.Log.Error("Failed to record post view history: " + err.Error())
    }
}
```

> 复用现有 `IncrementPostViewCountWithDedup` 的 5min 去重窗口（[view_lua.go](../pkg/server/storage/redis/view_lua.go)）：`newCount==0` 表示窗口内已计过，跳过 history；`newCount>0` 才记录。天然防刷新刷量，无需额外去重 key。

---

## 十、装配

### 10.1 `cmd/apps/server.go`（仿 8.10 节）

```go
// 8.12 Init History event Redpanda producer + consumer
if err := redpanda.InitHistoryEventProducer(); err != nil {
    logger.Log.Error("Failed to initialize history event producer: " + err.Error())
} else {
    logger.Log.Info("History event producer initialized successfully")
    go redpanda.StartHistoryEventConsumerWithRetry()
}
// 8.13 Init History Lua scripts in Redis
if err := redis.InitHistoryLuaScripts(); err != nil {
    logger.Log.Error("Failed to load history Lua scripts: " + err.Error())
} else {
    logger.Log.Info("History Lua scripts loaded successfully")
}
// 关闭(Shutdown 段):
redpanda.CloseHistoryEventProducer()
```

### 10.2 `pkg/composition/server.go`

```go
// RegisterDomainRoutes 内新增:
historySvc := newHistoryService(deps)
postSvc.SetHistoryRecorder(&postHistoryRecorder{delegate: historySvc})  // post → history
historySvc.SetPostFetcher(&historyPostFetcher{delegate: postSvc})       // history → post(ES)
registerHistory(root, historySvc, authCheck)

// newHistoryService:
func newHistoryService(deps *Deps) historyapp.HistoryService {
    cache := historyinfra.NewPostHistoryCache()
    repo := historyinfra.NewPostHistoryRepository(deps.DB.Get())
    publisher := historyinfra.NewHistoryEventPublisher()
    return historyapp.NewHistoryService(cache, repo, publisher)
}
```

### 10.3 `pkg/composition/facade_bridges.go`（新增两桥接器）

```go
// postHistoryRecorder: history.HistoryService → post.domain.HistoryRecorder
type postHistoryRecorder struct{ delegate historyapp.HistoryService }
func (f *postHistoryRecorder) RecordView(ctx, userID, postID) error { return f.delegate.RecordView(ctx, userID, postID) }

// historyPostFetcher: post.PostService → history.application.PostFetcher(ES)
type historyPostFetcher struct{ delegate postapp.PostService }
func (f *historyPostFetcher) SearchByIDs(ctx, postIDs) ([]postapp.PostListItem, error) {
    return f.delegate.SearchPostsByIDs(ctx, postIDs)
}
```

---

## 十一、实现步骤（文件清单）

**A. 数据库**
- [ ] `docs/db.md`：追加 `post_view_history` DDL（§三）。
- [ ] DB 执行 DDL。

**B. 配置**
- [ ] `pkg/conf/conf.go`：Redpanda struct 加 3 字段（§5.1）。
- [ ] `configs/config.yaml`：加 `history_event_topic / consumer_group / flush_interval`。

**C. Redis（pkg/server/storage/redis）**
- [ ] `constants.go`：`UserPostViewListPrefix` + `GetUserPostViewListKey`（§4.1）。
- [ ] `history_lua.go`（新）：`historyRecordScript` + `RecordPostView` + `ListPostViews` + `BackfillPostViews` + `InitHistoryLuaScripts`（§4.2）。

**D. MQ（pkg/server/storage/redpanda）**
- [ ] `producer.go`：`historyEventWriter` + `InitHistoryEventProducer` + `HistoryEventMessage` + `PublishPostViewHistoryEvent` + `CloseHistoryEventProducer`（§5.2）。
- [ ] `history_consumer.go`（新）：`HistoryEventAggregator` + `StartHistoryEventConsumer` + `batchUpdatePostViewHistory` + `StartHistoryEventConsumerWithRetry`（§5.3）。

**E. history 领域（新建 pkg/domains/history）**
- [ ] `domain/`：`history.go`(实体+错误)、`cache.go`(PostHistoryCache)、`repository.go`(PostHistoryRepository)、`publisher.go`(HistoryEventPublisher)（§6.1）。
- [ ] `application/service.go`：`HistoryService` + impl + `PostFetcher` + DTO + `orderByViewTime`（§6.2/6.3）。
- [ ] `infrastructure/`：`history_cache_redis.go`、`history_repository.go`、`history_event_publisher.go`。
- [ ] `interfaces/http/`：`handler.go` + `routes.go`（§七）。

**F. post 域**
- [ ] `domain/history_port.go`（新）：`HistoryRecorder`（§9.1）。
- [ ] `application/service.go`：`SetHistoryRecorder` + 字段 + `asyncIncrementView` 钩子（§9.2）。
- [ ] ES by IDs：`elasticsearch/post.go` `SearchPostsByIDs` + `PostSearcher.SearchByIDs` + `PostService.SearchPostsByIDs` + searcher impl（§八）。

**G. 装配**
- [ ] `cmd/apps/server.go`：producer/consumer/lua 启动 + 关闭（§10.1）。
- [ ] `pkg/composition/server.go`：`newHistoryService` + 注入 + `registerHistory`（§10.2）。
- [ ] `pkg/composition/facade_bridges.go`：`postHistoryRecorder` + `historyPostFetcher`（§10.3）。

**H. 验证**
- [ ] `go build ./...` 通过。
- [ ] 手动：看帖详情 → Redis `ZCARD user:view:posts:{uid}` +1；`GET /history/posts` 返回该帖置顶；重复看（>5min）→ `view_count`/`update_time` 更新；ZSET 满 500 后淘汰最旧；`FLUSHALL` 后 `GET /history/posts` 触发 DB 回源。

---

## 十二、风险与注意

1. **MQ 与 DB 最终一致**：Redis 即时写（列表权威），DB 经 MQ 秒级滞后落库。Redis 写成功但 MQ 失败 → DB 缺该条，但下次浏览补偿；冷启动回源 DB 可能少最近几秒的数据，可接受。
2. **ZSET 过期丢数据**：TTL 到期 ZSET 清空，冷启动回源 DB top500 恢复；DB 是持久权威。若可接受「过期即丢历史」，可去掉 DB 回源进一步简化（不推荐）。
3. **高频写入**：5min 去重窗口已大幅降频（同帖 5min 内仅 1 次 Redis ZADD + 1 条 MQ）；MQ consumer 再按 `(user,post)` 聚合 flush，DB 单事务批量 upsert。
4. **PK 生成**：consumer upsert 插入分支必须 `sharedomain.NewID()` 生成 UUIDv7（见 memory [[uuidv7-primary-key-generation]]）。
5. **offset 分页漂移**：浏览历史 ZSET 实时变动（新浏览移到顶部），offset 翻页可能重复/跳过。cap 500 + 「最近浏览」语义下可接受；若需严格不漂移，可改 DB keyset（牺牲 Redis 即时读）。
6. **个人可见性**：`/history/posts` 仅返回当前登录用户自己的记录（userID 取自 token），无越权。
7. **失败隔离**：`RecordView` 在 async goroutine 内，失败仅日志，不影响 `GetPostDetail` 详情返回（与现有 `asyncIncrementView` 一致）。

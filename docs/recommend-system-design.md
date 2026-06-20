# qubar 首页推荐系统 设计文档

> 决策口径（已与需求方确认）：**规则公式精排**（综合性能与准确率最优）／**向量召回延后二期**（pgvector，复用 PG）。

---

## 1. Context（背景与目标）

qubar 是基于 **Hertz + DDD** 的社区系统（圈子 + 帖子 + 评论 + 点赞）。当前首页只有 `GET /post/list`（ES 按 id 倒序 / 关键词相关度排序）和 `GET /circle/posts`（圈内热点/最新/精华）。**没有个性化推荐**：所有用户看到相同的时间序帖子流，新帖快速沉底，冷启动用户无引导，优质内容曝光不足。

本方案设计一个 **`GET /feed/recommend`** 首页推荐系统，目标：

- **准确率**：让每个用户看到「自己更可能感兴趣」的内容（已加入圈子、行为相似用户、全站优质）。
- **性能**：在线接口 **P99 < 20ms**，靠「离线/近线预计算召回池 + 在线只读 Redis ZSET 合并排序」实现。
- **复用**：最大化复用既有 ES / Redis / Redpanda 设施与三套已验证范式（`LikeEventAggregator` 近线聚合、`hot / (age_hours+2)^0.8` 热度衰减、`search_after` 游标分页）。
- **可演进**：精排抽象成接口、召回路可插拔，预留向量召回（二期）与模型精排位。

---

## 2. 现状与可复用资产

### 2.1 推荐可用特征（已有数据）

| 维度 | 来源 | 字段 |
|---|---|---|
| 内容特征 | `post/domain/post.go` | Type / Title / Summary / Content / MediaExtra / CircleID / UserID |
| 质量特征 | Redis `post:stats:{id}` Hash | view_count / comment_count / like_count / collect_count |
| 内容状态 | Post 实体 | IsPinned / IsEssence / IsLock / Status / CreateTime / LastReplyTime |
| 圈子热度 | `circle/domain/circle.go` + Redis `circle:stats:{id}` | Hot / MemberCount / PostCount / CategoryID |
| 用户兴趣（最强信号） | Redis `user_joined_circles:{id}` | 已加入圈子 ID 列表（已缓存，TTL 24h） |
| 行为（点赞） | Redis `user:like:posts:{id}` ZSET | score=Unix 毫秒，member=post_id |

### 2.2 三套可直接复用的范式

1. **近线聚合 consumer** — `pkg/server/storage/redpanda/like_consumer.go`：
   `LikeEventAggregator` = `time.Ticker` + `sync.Mutex` + `map` 增量聚合 + `stopChan` 优雅关停 + `flush()` 批量事务落库；`kafka.NewReader` 消费 + `StartXxxWithRetry` 重试。→ 推荐系统的近线行为 consumer、召回池增量更新直接照搬。

2. **热度衰减公式** — `pkg/server/storage/elasticsearch/post.go` `SearchCirclePosts` sortType=1 的 runtime_mappings script：
   `rank_score = hot / (age_hours + 2)^0.8`。→ 精排热度因子的基底。

3. **search_after 游标分页** — `elasticsearch/post.go` 全程使用。→ 推荐 feed 分页机制。

### 2.3 已确认缺失（需补 / 可延后见 §8）

用户浏览历史、用户收藏关系（仅 count 无关系表）、内容标签、用户关注关系、向量库、Prometheus/OTel、cron 框架。

---

## 3. 总体架构

核心思路：**把 ES 实时查询搬到 cron 离线预计算成召回池（Redis ZSET），在线只做「多路 ZRANGE 合并 → 粗排 → 精排 → 重排 → 游标分页」**，全程内存级运算，保毫秒级。

```
┌──────────────────────── 离线 / 近线（off-line / near-line）────────────────────────┐
│                                                                                      │
│  行为事件 view/like/comment/collect/share/expose                                      │
│      │  (复用 LikeEventPublisher 范式)                                                │
│      ▼                                                                               │
│  Redpanda topic: feed_behavior                                                       │
│      │                                                                               │
│      ▼                                                                               │
│  FeedBehaviorConsumer（复用 LikeEventAggregator：ticker+聚合+flush）                  │
│    ├─ batch INSERT user_behavior_log（PG）                                            │
│    ├─ ZADD user:behavior:{user}      （近 N 条行为 post，score=ts*权重）               │
│    └─ ZADD item:interactors:{post}   （近 M 个交互用户，CF 倒排）                     │
│                                                                                      │
│  Scheduler（自建 pkg/server/scheduler，复用 ticker 模式，多 Job 注册）                 │
│    Job-1 全站热度榜 10min  → feed:pool:global      （Top 500）                        │
│    Job-2 分圈子池  10min  → feed:pool:circle:{c}   （活跃圈各 Top 200）               │
│    Job-3 分类池    10min  → feed:pool:category:{c} （各分类 Top 300）                 │
│    Job-4 精华池    30min  → feed:pool:essence      （Top 300）                        │
│    Job-5 用户画像  5min   → user:profile:{user}    （圈子兴趣权重，增量）             │
│    Job-6 CF 共现   每天03:00 → cf_item_sim 表（item-item）                            │
│    Job-7 行为日志清理 每天04:00 → 删 90d 前 user_behavior_log                         │
│                                                                                      │
└──────────────────────────────────────────────────────────────────────────────────────┘
                                      │ 预计算产物（Redis ZSET/Hash + PG 表）
                                      ▼
┌──────────────────────── 在线（GET /feed/recommend，目标 P99<20ms）──────────────────┐
│                                                                                      │
│  Req(user_id, cursor, size, scene)                                                   │
│      ▼                                                                               │
│  【多路召回】并发 goroutine，各取候选，全部 ZRANGE 内存级                              │
│    R1 已加入圈子  ← user_joined_circles:{user} × feed:pool:circle:{c}  （主信号）    │
│    R2 协同过滤    ← user:behavior:{user} Top-K 种子 → cf_item_sim 查邻居             │
│    R3 全站热度    ← feed:pool:global                                   （兜底+探索） │
│    R4 精华        ← feed:pool:essence                                  （质量保障） │
│    R5 分类兴趣    ← 用户加入圈子 → category_id → feed:pool:category:{cat}            │
│      ▼                                                                               │
│  【合并+粗排】去重 map + 过滤删除/封禁（走 post:stats 缓存）+ 召回路权重×hot_decay     │
│      ▼                                                                               │
│  【精排】每候选取 final_score（§6 公式，纯 Redis/stats 元数据，无外部 IO）            │
│      ▼                                                                               │
│  【重排】曝光去重(7d) → 已收藏剔除 → 多样性打散(同 circle/作者/type 连续≤2)          │
│          → 业务加权(置顶/精华/负反馈) → 运营强插位(预留)                               │
│      ▼                                                                               │
│  【分页】search_after 风格 cursor = base64(score, post_id, pool_epoch)                │
│      ▼                                                                               │
│  Resp：posts[]（复用 PostListItem 结构 + recommend_reason）+ cursor + has_more        │
│      ▼                                                                               │
│  【异步回写】曝光事件 → Redpanda feed_behavior（闭环）                                │
│                                                                                      │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

**冷启动**：新用户 `user:behavior` 空、`user_joined_circles` 可能空 → R2/R1 失效，降级到 R3（全站热度）+ R4（精华）+ R5（注册时选的 category）。保证新用户立刻有内容。

---

## 4. 推荐接口契约

### 4.1 请求

```
GET /feed/recommend
Header: Authorization（Sa-Token）
Query:
  size     int     每页数量，默认 20，上限 50
  cursor   string  上一页返回 cursor，首页空。base64(JSON{score, post_id, pool_epoch})
  scene    string  场景，默认 "home"；预留 "circle_focus" / "explore"
  refresh  int     1=强制刷新（忽略 cursor 重新拉首页）
```

### 4.2 响应

```jsonc
{
  "posts": [
    {
      "id": "uuid", "circle_id": "uuid", "circle_name": "...", "circle_avatar": "...",
      "user_id": "uuid", "author_name": "...", "author_avatar": "...",
      "type": 1, "title": "...", "summary": "...",
      "view_count": 1234, "like_count": 56, "comment_count": 7, "collect_count": 3,
      "is_pinned": 0, "is_essence": 0, "images": ["url"],
      "create_time": "RFC3339", "is_liked": false,
      "recommend_reason": {
        "source": "joined_circle",   // joined_circle | cf | global_hot | essence | category
        "circle_name": "Go 语言",     // source=joined_circle/category 时给
        "score": 0.87                // 调试/埋点
      }
    }
  ],
  "cursor": "eyJzIjowLjg3...",
  "has_more": true,
  "request_id": "...",
  "refreshed_at": 1718800000
}
```

帖子 VO 复用现有 `PostListItem`（`post/application/service.go`），追加 `recommend_reason`。

### 4.3 游标与池刷新

- cursor = `[final_score DESC, post_id DESC, pool_epoch]` 元组 base64。
- 同一召回池周期内（10min），每帖分数由公式唯一确定 → 翻页连续不跳变。
- cursor 内 `pool_epoch` 与当前 `feed:pool:epoch` 差异超阈值 → 强制 `refresh` 重拉首页（避免池刷新后 cursor 失效）。

---

## 5. 多路召回设计

| 路 | 数据来源 | 计算方式 | 存储 | 更新 |
|---|---|---|---|---|
| **R1 已加入圈子**（主信号） | `user_joined_circles:{user}` + `feed:pool:circle:{c}` | 取用户加入圈子，对每圈 ZRANGE Top-N 合并 | ZSET（已有+新建） | cron 10min |
| **R2 协同过滤 CF** | `user:behavior:{user}` Top-K 种子 → `cf_item_sim` 表查邻居 | item-item 共现，离线计算 | PG 表 + 内存 LRU | cron 每天 + 活跃用户 30min 增量 |
| **R3 全站热度** | `post:stats:*` + Post.hot | hot_decay（§6） | `feed:pool:global` | cron 10min |
| **R4 精华** | ES `is_essence=1` + hot | ES 查询 + ZADD | `feed:pool:essence` | cron 30min |
| **R5 分类兴趣** | 用户加入圈子 → category_id → `feed:pool:category:{cat}` | 二跳映射 | ZSET | cron 10min |

**召回配额**：为防热帖马太效应，全站池 R3 占比 ≤ 30%，R1/R2（个性化）优先。

---

## 6. 精排打分公式（规则公式方案）

沿用已验证的 `hot / (age_hours + 2)^0.8` 作为热度衰减基底，扩展为五因子加权：

```
final_score =
    0.40 * hot_decay          # 热度时间衰减（沿用现有公式）
  + 0.25 * personalization    # 个性化匹配
  + 0.20 * quality            # 质量分（互动率）
  + 0.10 * freshness          # 新鲜度
  + business_boost            # 业务加权（加性）

hot_decay = normalize( hot / (age_hours + 2)^0.8 )
  hot        = max(post.hot, like_count*2 + comment_count*3 + collect_count*4)  # Redis stats 实时值
  age_hours  = (now - create_time) / 3600
  normalize  = 池内 min-max 归一到 [0,1]

personalization = 召回路权重(joined_circle=1.0, cf=0.9, category=0.6, essence=0.7, global=0.3)
                × (用户对该帖 circle 兴趣权重 / 用户最大 circle 权重)   # 来自 user:profile:{user}

quality = normalize( (like*1 + comment*2 + collect*3) / max(1, view_count/50) )  # 互动率

freshness = exp(-age_hours / 72)   # 3 天半衰期

business_boost =
    + 0.15 if is_essence
    + 0.30 if is_pinned
    - 0.50 if in user:dislike:{user}        # 负反馈强降权
    - 0.20 if 作者在用户近期负反馈列表
```

- 权重 α/β/γ/δ 写 `recommend/domain` 常量，可配置化（二期接 ABTest）。
- 所有因子可从 Redis（`post:stats`、`user:profile`）+ 候选元数据算出，**无外部 IO** → 精排 200 候选 < 2ms。

---

## 7. 重排规则（顺序执行）

1. **曝光去重**：剔除 `feed:viewed:{user}` 中 7d 已曝光 post（一次 ZRANGE）。短期去重复用既有 `post:viewdedup`，不在推荐层重复。
2. **已收藏剔除**：已收藏帖剔除（避免重复推荐）；已点赞帖降权不剔除。
3. **多样性打散（滑动窗口）**：同 circle 连续 ≤2、同作者连续 ≤2、同 type（视频/投票）连续 ≤2。贪心交换实现。
4. **业务置顶**：`is_pinned` 受多样性约束置顶。
5. **运营强插位**：首位/第 5 位预留（读 `feed:promoted` list，一期可空）。
6. **回写曝光**：返回结果 ZADD 进 `feed:viewed:{user}`（7d TTL）。

---

## 8. 数据补全：新表 / Key / Topic

### 8.1 必须补（一期）

```go
// pkg/domains/recommend/domain/behavior.go —— 用户行为日志（CF/负反馈/效果指标依赖）
type UserBehaviorLog struct {
    sharedomain.BaseModel
    UserID   uuid.UUID `gorm:"index:idx_user_time,priority:1"`
    PostID   uuid.UUID `gorm:"index:idx_post;index:idx_user_time,priority:2"`
    CircleID uuid.UUID `gorm:"index:idx_circle"`
    Behavior int16      // 1=view 2=like 3=comment 4=collect 5=share 6=expose 7=dislike
    Weight   float64    // view=1 like=3 comment=5 collect=5 share=4 expose=0.1
    DwellTime int       // 停留毫秒（view 记录，质量分用）
    CreateTime time.Time `gorm:"index:idx_user_time,priority:3"`
}

// pkg/domains/recommend/domain/collect.go —— 收藏关系（现仅有 count）
type PostCollect struct {
    sharedomain.BaseModel
    UserID uuid.UUID `gorm:"uniqueIndex:uk_user_post,priority:1"`
    PostID uuid.UUID `gorm:"uniqueIndex:uk_user_post,priority:2;index:idx_post"`
    CircleID uuid.UUID `gorm:"index"`
    CreateTime time.Time
}

// pkg/domains/recommend/domain/cf.go —— item-item CF 共现（离线计算）
type CFItemSim struct {
    ItemID     uuid.UUID `gorm:"uniqueIndex:uk_item,priority:1;type:varchar(36)"`
    NeighborID uuid.UUID `gorm:"uniqueIndex:uk_item,priority:2"`
    Score      float64   // 共现频次 / sqrt(pop_a * pop_b)
    UpdateTime time.Time
}
```

迁移走 GORM AutoMigrate（与现有实体一致，`pkg/server/storage/db/pgsql/connect.go`）。

### 8.2 新 Redis key（追加 `pkg/server/storage/redis/constants.go`，遵循现有 `Prefix` const + `GetXxxKey()` 风格）

```
feed:pool:global          ZSET  全站热度 Top 500
feed:pool:circle:{circle} ZSET  圈子池 Top 200，TTL 30min
feed:pool:category:{cat}  ZSET  分类池 Top 300，TTL 30min
feed:pool:essence         ZSET  精华池 Top 300，TTL 60min
feed:pool:epoch           str   池刷新时间戳（cursor 校验）
user:profile:{user}       Hash  field=circle_id value=兴趣权重，TTL 6h
user:behavior:{user}      ZSET  近 200 条行为 post，score=ts，TTL 30d
item:interactors:{post}   ZSET  近 200 交互用户，score=ts，TTL 30d
feed:viewed:{user}        ZSET  7d 已曝光 post，score=ts，TTL 7d
user:dislike:{user}       ZSET  30d 主动不喜欢，TTL 30d
```

命名风格与 `post:viewdedup:` `user:like:posts:` `circle:stats:` 一致（小写冒号分层）。TTL 略大于 cron 间隔，防 job 失败空窗。

### 8.3 新 Redpanda topic（追加 `pkg/conf/conf.go` 的 `Redpanda` struct + `configs/config.yaml`）

```yaml
redpanda:
  feed_behavior_topic: "feed_behavior"
  feed_behavior_consumer_group: "feed_behavior_group"
  feed_behavior_flush_interval: 5
```

Producer/Consumer 复用 `InitLikeEventProducer` / `LikeEventAggregator` 范式，新建 `pkg/server/storage/redpanda/feed_behavior_producer.go`、`feed_behavior_consumer.go`。

### 8.4 可延后（二期）

内容标签/话题、用户关注关系、向量库（pgvector）、Prometheus/OTel、行为日志按月分区。

---

## 9. cron 定时任务（自建调度器）

新建 `pkg/server/scheduler/scheduler.go`：轻量调度器，封装 `time.Ticker` + 多 Job 注册，复用 `LikeEventAggregator` 的 `stopChan` 优雅关停模式（不引入新依赖）。

```go
type Job struct {
    Name     string
    Interval time.Duration
    At       string // "03:00"=每日定时；空=按 Interval
    Run      func(ctx context.Context) error
}
```

| Job | 频率 | 产物 | 落地 |
|---|---|---|---|
| RefreshGlobalHotPool | 10min | 全站热度 Top 500 | `feed:pool:global` |
| RefreshCirclePools | 10min | 活跃圈各 Top 200 | `feed:pool:circle:{c}` |
| RefreshCategoryPools | 10min | 各分类 Top 300 | `feed:pool:category:{cat}` |
| RefreshEssencePool | 30min | 精华 Top 300 | `feed:pool:essence` |
| RefreshUserProfile | 5min | 活跃用户兴趣画像（增量） | `user:profile:{u}` |
| ComputeCFItemSim | 每天 03:00 | item-item 共现矩阵 | `cf_item_sim` 表 |
| CleanupBehaviorLog | 每天 04:00 | 删 90d 前日志 | `user_behavior_log` |

热度池计算：读 `post:stats:*` Hash（已实时）+ ES `SearchPosts`（无 keyword，过滤 `deleted=0, status=1`），对 Top-N 算 hot_decay 后 ZADD。Job-6（CF）最重，放凌晨，扫近 30d `user_behavior_log` 聚合 item 共现。**R2 待 `user_behavior_log` 积累 2-4 周再启用**（CF 冷启动）。

---

## 10. recommend 领域分层落位（文件清单）

遵循现有四层 DDD + `composition` 装配范式。

### 新建

```
pkg/domains/recommend/
├── domain/
│   ├── behavior.go          # UserBehaviorLog
│   ├── collect.go           # PostCollect
│   ├── cf.go                # CFItemSim
│   ├── ports.go             # RecallSource / Ranker / Reranker / BehaviorRepo 接口
│   └── ranking.go           # 打分常量与公式因子
├── application/
│   ├── service.go           # RecommendService（编排：召回→粗排→精排→重排→分页）
│   ├── dto.go               # FeedReq / FeedResp / RecommendReason
│   └── errors.go
├── infrastructure/
│   ├── recall_joined_circle.go   recall_cf.go   recall_global_hot.go
│   ├── recall_essence.go         recall_category.go      # 各召回路（实现 RecallSource）
│   ├── ranker_default.go         # 精排公式（实现 Ranker）
│   ├── reranker_default.go       # 重排（实现 Reranker）
│   ├── behavior_repo_pg.go  collect_repo_pg.go  cf_repo_pg.go
│   ├── recall_pool_cache_redis.go  profile_cache_redis.go  exposure_cache_redis.go
│   ├── behavior_event_publisher.go  # 调 redpanda producer
│   └── pool_refresher.go           # cron job 执行体
└── interfaces/http/
    ├── handler.go           # GET /feed/recommend
    └── routes.go            # RegisterRoutes(root, svc, authCheck)

pkg/server/scheduler/scheduler.go                          # 轻量 cron
pkg/server/storage/redpanda/feed_behavior_producer.go      # 复用 InitLikeEventProducer
pkg/server/storage/redpanda/feed_behavior_consumer.go      # 复用 LikeEventAggregator
```

### 修改（最小侵入，贴现有规范）

- `pkg/server/storage/redis/constants.go` — 追加 `feed:pool:*` / `user:profile:` / `user:behavior:` / `item:interactors:` / `feed:viewed:` / `user:dislike:` 前缀 const + `GetXxxKey()`（与 `GetPostStatsKey` 风格一致）。
- `pkg/conf/conf.go` — `Redpanda` struct 追加 `FeedBehaviorTopic / FeedBehaviorConsumerGroup / FeedBehaviorFlushInterval`。
- `configs/config.yaml` — 追加上述 3 项。
- `cmd/apps/server.go` — 启动序列追加（紧跟现有 like event 之后）：`InitFeedBehaviorProducer` / `go StartFeedBehaviorConsumerWithRetry` / `scheduler.Start()`；关停追加对应 Close/Stop。
- `pkg/composition/server.go` — `RegisterDomainRoutes` 追加 `newRecommendService(deps)` + 互注 Facade（recommend ← post/user/circle）+ `registerRecommend(root, recSvc, authCheck)`。bridge struct 仿现有 `circleUserFacade{delegate:...}` / `postMediaFetcherForCircle` 模式。
- `pkg/composition/deps.go` — 若 scheduler 作为共享依赖，在此构造。
- `pkg/server/storage/db/pgsql/connect.go` — AutoMigrate 列表追加 `UserBehaviorLog` / `PostCollect` / `CFItemSim`。
- `pkg/domains/post/application/service.go` — `GetPostDetail` 的 `asyncIncrementView` 旁追加 `publishFeedBehavior(view, dwell)`；收藏增删发 collect 事件。
- `pkg/domains/like/application/service.go`、`comment/application/service.go` — 点赞/评论成功后发 feed_behavior 事件。

### 关键参考文件（实现时对照）

- [pkg/composition/server.go](../pkg/composition/server.go)（Facade 互注 + register 模式）
- [pkg/server/storage/redpanda/like_consumer.go](../pkg/server/storage/redpanda/like_consumer.go)（近线 consumer 范式）
- [pkg/server/storage/elasticsearch/post.go](../pkg/server/storage/elasticsearch/post.go)（hot 公式 + search_after）
- [pkg/server/storage/redis/constants.go](../pkg/server/storage/redis/constants.go)（key 规范）
- [cmd/apps/server.go](../cmd/apps/server.go)（启动序列）

---

## 11. 分阶段实施路线

### Phase 1 — MVP（核心推荐闭环）

目标：上线 `GET /feed/recommend`，三路召回（R1 已加入圈子 + R3 全站热度 + R4 精华）+ 精排公式 + 重排，效果优于现有 `/post/list` 时间序。

1. 领域骨架：新建 `pkg/domains/recommend` 四层。
2. 基础设施补全：新表 + AutoMigrate、新 Redis key、新 Redpanda topic + producer/consumer、自建 scheduler。
3. 召回池 cron：Job-1/2/3/4（global/circle/category/essence）。
4. 在线 RecommendService：多路并发召回 + 精排公式 + 重排 + search_after 分页。
5. 事件闭环：`GetPostDetail`/点赞/评论/收藏处埋点发 `feed_behavior`；曝光埋点在 recommend 返回后异步发。
6. 跨领域 Facade：recommend 注入 PostFacade（复用 post 的批量组装逻辑）/ UserFacade / CircleFacade。
7. 接口注册：`GET /feed/recommend` 经 `composition.RegisterDomainRoutes` 挂载。

### Phase 2 — 向量召回 + 模型精排（二期）

1. **pgvector**：PG 扩展，对 title+summary 跑 embedding（本地小模型/API），存 `post_embedding` 表 `vector(384/768)`，ANN 召回路 R6；用户向量由近期行为帖 embedding 聚合。
2. **模型精排**：精排公式替换为 GBDT/LR（一期已抽象 Ranker 接口，平滑替换），离线训练 + 在线推理（ONNX Runtime 或 Go 内嵌线性模型）。
3. ABTest 框架：流量分桶对比 CTR/停留。
4. 内容标签：打标管线丰富画像。
5. 监控：Prometheus + Grafana。

---

## 12. 验证方式

**单测**：
- 精排公式：mock post + stats + profile，断言排序符合预期（essence > 普通、新帖 > 老帖、已加入圈帖 > 全站）。
- 重排：构造 3 连同 circle 候选，断言打散后连续 ≤2。
- 召回池刷新：mock ES + Redis，断言 ZADD score = hot_decay 归一值。
- cursor 分页：50 候选翻 3 页，断言无重复、连续。

**集成测**：启动 scheduler + 填测试数据 → 调 `/feed/recommend` → 断言结构、reason、去重；冷启动空行为用户断言返回非空。

**压测**：wrk/vegeta 1000 QPS 打 `/feed/recommend`，目标 P99 < 20ms，定位瓶颈（预期在 facade 批量查 user/circle，可加二次缓存）；cron 单次执行 < 5s。

**效果指标**（埋点用 `user_behavior_log`）：
- CTR（点击/曝光：expose vs view 详情）
- 平均停留时长（dwell_time 中位数）
- 去重率（重排剔除已曝光比例）
- 召回路命中分布（reason.source 占比，监控 R1/R3 失衡）
- 对照组：与 `/post/list` 时间序离线回放对比 CTR 提升

---

## 13. 风险与对策

| 风险 | 对策 |
|---|---|
| 召回池刷新失败空窗 | ZSET TTL > cron 间隔（30min vs 10min），失败告警；空池降级 ES 实时查 |
| CF 冷启动（行为少） | Phase 1 不上线 R2，待 `user_behavior_log` 积累 2-4 周再启 Job-6 |
| 精排分数池更新前后跳变 | cursor 内嵌 pool_epoch，跨 epoch 强制 refresh |
| Redis 内存（7d 曝光去重） | `feed:viewed:{user}` 仅存 post_id（16B），7d 单用户数千条可控，超限 LRU 截断 |
| 热帖马太效应 | R3 全站池配额 ≤30%，personalization 权重高于 hot |

---

## 关键设计决策小结

1. **规则公式精排**（综合性能+准确率最优），预留 Ranker 接口二期接模型。
2. **预计算召回池**：ES 实时查询搬 cron 离线，在线只读 ZSET → 毫秒级。
3. **复用三范式**：`LikeEventAggregator`（近线）、`hot_decay`（精排基底）、`search_after`（分页）、`view dedup`（去重）。
4. **数据补全最小化**：仅补行为日志 + 收藏关系 + 曝光去重三必须项；标签/关注/向量延后。
5. **冷启动兜底**：新用户降级到全站热度 + 精华 + 注册 category。
6. **可演进**：Ranker 接口可插拔、RecallSource 可插拔、预留向量召回（pgvector）+ 模型精排位。

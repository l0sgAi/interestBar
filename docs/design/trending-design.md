# 热点（Trending）领域 设计方案

> 目标：新增独立领域 `pkg/domains/trending`，对外提供 `GET /trending` 聚合接口，
> 返回**近期热门的圈子 / 帖子 / 用户**三类榜单，支撑前端「热点」页面。
>
> 时间窗：**24 小时（24h）+ 7 天（7d）** 双榜可切换。
> 推送机制：**榜单数据刷新策略**——后台 job 周期性从 ES 聚合计算 Top-N，写入 Redis ZSET 榜单；
> 读路径直接读 ZSET，不引入 WebSocket/SSE。
>
> 设计范式与 [active-circles-design.md](active-circles-design.md) / [hot-sync-design.md](hot-sync-design.md) 一致。

---

## 一、目标与领域定位

### 1.1 领域定位

`trending` 是**跨域编排器（无聚合根）**，职责是「聚合三类热点榜单 + 跨域回填展示信息」，
与 `recommend`（[pkg/domains/recommend/domain/ports.go:1](../pkg/domains/recommend/domain/ports.go#L1) 注释定义的「跨域编排器」）同范式：

- 本包不 import `post` / `circle` / `user`，所有跨域依赖经 `domain/ports.go` 的接口抽象，由 `pkg/composition` 注入桥接器（避免环依赖）。
- 同域 infra 只有两种：Redis ZSET 榜单存储、ES 聚合计算器。
- 后台同步任务（`TrendingRankSyncer`）放在 `pkg/server/storage/redpanda/`，与 `CircleHotSyncer` 同层。

### 1.2 数据流（一图）

```
                   ┌─────────────────────────────────────────┐
   post ES 索引    │  TrendingRankSyncer（redpanda 包）       │
   (CDC 已就位) ──▶│  每 5min：ES terms/sum(hot) 聚合          │
                   │  → ZADD 覆盖写 Redis ZSET + 裁剪 Top-N   │
                   └────────────────────┬────────────────────┘
                                        │ 写
                                        ▼
   ┌────────────────────────────────────────────────────────┐
   │  Redis ZSET 榜单（6 个）                                │
   │  trending:{post|circle|user}:{24h|7d}                  │
   │  + trending:meta:{key}  string(Unix秒, 刷新时间)        │
   └────────────────────┬───────────────────────────────────┘
                        │ 读（ZREVRANGE）
                        ▼
   ┌────────────────────────────────────────────────────────┐
   │  TrendingService.GetTrending                            │
   │  ZSET 拿 member+score → 跨域 Facade 回填展示信息         │
   │  → 组装 TrendingBoard（posts+circles+users）            │
   └────────────────────┬───────────────────────────────────┘
                        │
                        ▼
              GET /trending?window=24h&size=20&section=all
```

### 1.3 为什么是独立领域（不复用 recommend）

| 维度 | recommend | trending |
|---|---|---|
| 服务对象 | 首页信息流（个性化） | 热点页（全局榜单） |
| 个性化 | 是（用户候选池/CF） | 否（全局同榜单） |
| 数据形态 | 帖子列表（单一） | 圈子+帖子+用户（三类聚合） |
| 读路径 | 候选池 LIST offset 翻页 | ZSET 榜单 ZREVRANGE |
| 写路径 | miss 时 5 路召回重建 | 后台 job 周期覆盖 |

职责差异大，强行塞进 recommend 会让其同时背负「个性化推荐」+「全局热点榜」，故独立成域。
两者仍可共享只读跨域端口（`PostHydrator` / `InteractionChecker`）。

---

## 二、数据源与打分信号

### 2.1 唯一数据源：post ES 索引

`PostDocument`（[pkg/server/storage/elasticsearch/post.go:13](../pkg/server/storage/elasticsearch/post.go#L13)）已含本方案所需全部字段：

```go
type PostDocument struct {
    ID           string `json:"id"`
    CircleID     string `json:"circle_id"`   // ★ 圈子榜聚合键
    UserID       string `json:"user_id"`     // ★ 用户榜聚合键
    Hot          int    `json:"hot"`         // ★ 打分信号（hot 子系统产出）
    CreateTime   string `json:"create_time"` // ★ 时间窗过滤
    Deleted      int16  `json:"deleted"`     // 过滤
    Status       int16  `json:"status"`      // 过滤（仅 status=1 已发布）
    ...
}
```

> `hot` 由现有热度子系统产出：互动事件 → `ApplyHotDelta`（[hot_lua.go:33](../pkg/server/storage/redis/hot_lua.go#L33)）→
> `PostHotAggregator` 批量落 `domains.post.hot`（[hot_consumer.go:182](../pkg/server/storage/redpanda/hot_consumer.go#L182)）→
> CDC 同步进 ES。**本方案复用此链路，不新增 MQ topic、不改 hot 计算**。

### 2.2 三类榜单打分信号

| 榜单 | 聚合键 | 打分（score） | ES 方式 |
|---|---|---|---|
| 热门帖子 | `id`（post 自身） | post 的 `hot` | hits 按 `hot desc`（窗口 filter） |
| 热门圈子 | `circle_id` | 窗口内该圈子所发帖的 `Σ hot` | terms 聚合 + 子聚合 `sum(hot)` |
| 热门用户 | `user_id` | 窗口内该用户所发帖的 `Σ hot` | terms 聚合 + 子聚合 `sum(hot)` |

口径一致性：三类榜单都以 `hot` 为统一热度尺度，差异只在「帖子本身的热」vs「聚合到圈/人的热」。
用户/圈子无需自己的 hot 列（见 §2.3）。

### 2.3 为什么不给 user 加 hot 列、不直接用 circle.hot 累积列

| 候选信号 | 来源 | 是否带时间窗 | 选用 |
|---|---|---|---|
| 窗口内 `Σ hot`（聚合） | post ES 索引 terms+sum | ✅ | ✅ 本方案 |
| `domains.circle.hot` 累积 | [hot-sync-design.md O4](hot-sync-design.md)：`hot = GREATEST(hot+Δ,0)` 只增不减、永不衰减 | ❌ 终身累积 | ❌（老爆款霸榜，[active-circles §一](active-circles-design.md)已论证） |
| `domains.users.hot` 新增列 | 需改 user 表 schema + 新增 fan-out（post_hot 消费者写 user） | ❌ 累积 | ❌（实现量大、与窗口语义不符） |
| `circle:hot:{cid}` Δ 累加器 | [constants.go:124](../pkg/server/storage/redis/constants.go#L124)：34min GETDEL 清零，无滚动窗口历史 | ❌ 无历史 | ❌ |

**结论**：唯一带时间窗、语义干净、零 schema 变更的方案 = 在 post ES 索引上做 `terms + sum(hot)` 聚合。

---

## 三、ES 聚合查询设计

### 3.1 圈子榜 / 用户榜聚合体（核心，两者结构同构）

> 以圈子榜 24h 为例；用户榜把 `by_circle`/`circle_id` 换成 `by_user`/`user_id` 即可。

```jsonc
GET pg.public.post/_search
{
  "size": 0,
  "query": {
    "bool": {
      "filter": [
        { "term":  { "deleted": 0 } },
        { "term":  { "status": 1 } },
        { "range": { "create_time": { "gte": "now-24h/h" } } }
      ]
    }
  },
  "aggs": {
    "by_circle": {
      "terms": {
        "field": "circle_id",
        "size": 500,
        "order": { "hot_sum": "desc" }
      },
      "aggs": {
        "hot_sum": { "sum": { "field": "hot" } }
      }
    },
    "active_total": {
      "cardinality": { "field": "circle_id", "precision_threshold": 1000 }
    }
  }
}
```

要点（对照 [active-circles §四](active-circles-design.md)）：
- `size: 0`——不要 hits，只要聚合桶，省带宽/解析。
- `filter`（非 must）——纯结构化过滤，跳过相关性打分，命中 ES request cache。
- `terms.size = 500`（**maxScan**）——只排前 500 个，覆盖任何热点榜 UI；超出截断（见 §10）。
- `order: { hot_sum: desc }`——按子聚合 `sum(hot)` 降序，而非 `_count`（区别于 active-circles 按发帖数排序）。
- **无 `bucket_sort`**：job 阶段取全部 Top-500 入 ZSET；分页在读路径由 `ZREVRANGE start stop` 完成（ZSET 天然有序，比 ES bucket_sort 更适合反复切片）。
- `active_total`——`cardinality` 给近似活跃总数，可选写入 meta 供前端展示。

> 7d 窗口把 `now-24h/h` 改为 `now-7d/d`。job 阶段 size 固定取 `maxScan=500`，不传分页参数。

### 3.2 帖子榜聚合体

帖子榜直接取 hits（post 自身就是聚合单元），按 `hot desc` + 时间窗 filter：

```jsonc
GET pg.public.post/_search
{
  "size": 500,
  "query": {
    "bool": {
      "filter": [
        { "term":  { "deleted": 0 } },
        { "term":  { "status": 1 } },
        { "range": { "create_time": { "gte": "now-24h/h" } } }
      ]
    }
  },
  "sort": [
    { "hot": { "order": "desc" } },
    { "id":  { "order": "desc" } }      // 二级稳定序
  ],
  "_source": ["id", "hot"]
}
```

- `size=500`（同 maxScan）：job 一次取够 Top-500 入 ZSET。
- 二级排序 `id desc` 保证 hot 并列时序稳定。
- `_source` 只取 `id`+`hot`，省传输；展示信息由读路径 `PostHydrator` 回填。

> 与首页 hot tab 的区别：首页 hot tab 用 `rank_score = hot/(age_h+2)^0.8` 时间衰减（[post.go:739](../pkg/server/storage/elasticsearch/post.go#L739)），
> 面向「信息流长期热度」；本榜已由 `create_time` 时间窗显式圈定近期，**直接用原始 hot 排序**，语义更直白（窗口内谁最热）。

### 3.3 前置确认（实现第一步，S0）

`AggregateActiveCircles`（[post.go:586](../pkg/server/storage/elasticsearch/post.go#L586)）已假定 `circle_id` 为 keyword、`create_time` 为 date。
本方案复用同样假设，且新增对 `user_id`（keyword）的依赖。上线前必须：

```bash
GET pg.public.post/_mapping
# 确认 circle_id / user_id = keyword（或 text 带 .keyword 子字段）
# 确认 create_time = date
# 确认 hot = long/integer
```

不合规则加 ES index template（一次性运维动作，不改 Go）。若 `user_id`/`circle_id` 是 text，聚合字段改用 `.keyword`。

### 3.4 分页策略

- 读路径：`ZREVRANGE trending:{dim}:{window} start stop WITHSCORES`，`start=offset`、`stop=offset+size-1`。
- 游标用 `offset`（非 search_after）：ZSET 本身按 score 全序，offset 切片最自然。
- **翻页期间数据变动导致排名漂移**：可接受（趋势榜本性），文档与 UI 注明（对齐 [active-circles §4.3](active-circles-design.md)）。

---

## 四、Redis 榜单存储设计

### 4.1 Key 规范（预备加入 [constants.go](../pkg/server/storage/redis/constants.go)）

```go
// TrendingPrefix 热点榜单 ZSET key 前缀。
// 完整 key 格式: trending:{dimension}:{window}
//   dimension = post | circle | user
//   window    = 24h | 7d
// ZSET: member=实体 ID(uuid 字符串), score=热度(post 自身 hot / 圈子&用户为 Σhot)。
// 由 TrendingRankSyncer 周期性 ZADD 覆盖重写 + ZREMRANGEBYRANK 裁剪到 top_n；
// 不设 TTL（job 覆盖刷新），不走 GETDEL（与 circle:hot Δ 累加器语义不同）。
const TrendingPrefix = "trending:"

// TrendingMetaPrefix 榜单刷新时间戳 key 前缀。
// 完整 key: trending:meta:{dimension}:{window}（string, Unix 秒）。
// 读路径返回 refreshed_at，供前端显示「X 分钟前更新」。
const TrendingMetaPrefix = "trending:meta:"

// GetTrendingKey 获取热点榜单 ZSET 的完整 key。
func GetTrendingKey(dimension, window string) string {
	return TrendingPrefix + dimension + ":" + window
}

// GetTrendingMetaKey 获取热点榜单刷新时间戳的完整 key。
func GetTrendingMetaKey(dimension, window string) string {
	return TrendingMetaPrefix + dimension + ":" + window
}
```

6 个 ZSET + 6 个 meta string：
| key | member | score |
|---|---|---|
| `trending:post:24h` / `:7d` | post_id | post.hot |
| `trending:circle:24h` / `:7d` | circle_id | Σhot |
| `trending:user:24h` / `:7d` | user_id | Σhot |

### 4.2 写入策略：覆盖式重写（job 阶段）

```go
// BoardStore.Rewrite 伪代码（infrastructure/board_store_redis.go）
func (s *boardStoreRedis) Rewrite(ctx, dimension, window string, items []ScoredID) error {
	key := redispkg.GetTrendingKey(dimension, window)
	// 1. ZADD 覆盖：members 用 0..1 浮点 score（Σhot 转 float64）
	members := make([]*redis.Z, 0, len(items))
	for _, it := range items {
		members = append(members, &redis.Z{Score: it.Score, Member: it.ID})
	}
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, key)                       // 先清空，避免旧成员残留（ZADD 无法删除已落榜者）
	pipe.ZAdd(ctx, key, members...)          // 写入本轮 Top-N
	pipe.Set(ctx, redispkg.GetTrendingMetaKey(dimension, window), time.Now().Unix(), 0)
	_, err := pipe.Exec(ctx)
	return err
}
```

> 为什么 `DEL + ZADD` 而非 `ZADD + ZREMRANGEBYRANK`：前者一步到位且语义清晰（榜单是全量重算快照，非增量累加）。
> `TxPipeline` 保证原子。job 并发只有一个（mu 互斥 + 单 goroutine），无竞态。
>
> 与 `circle:hot:{cid}` Δ 累加器（[circle_hot_syncer.go](../pkg/server/storage/redpanda/circle_hot_syncer.go) 用 GETDEL 读后清零）的根本区别：
> 那是「增量累加 → 定时落库」，本榜是「全量重算 → 覆盖快照」，故无累加、无清零、无 TTL。

### 4.3 读策略（service 阶段）

```go
// BoardStore.Range 返回有序 [(id, score)]，按 score 降序。
func (s *boardStoreRedis) Range(ctx, dimension, window string, offset, size int64) ([]ScoredID, error) {
	key := redispkg.GetTrendingKey(dimension, window)
	zs, err := s.client.ZRevRangeWithScores(ctx, key, offset, offset+size-1).Result()
	// zs 已按 score 降序；同 score 按 member 字典序（uuid 字符串），可接受
	...
}
func (s *boardStoreRedis) RefreshedAt(ctx, dimension, window string) (int64, error) {
	v, err := s.client.Get(ctx, redispkg.GetTrendingMetaKey(dimension, window)).Int64()
	...
}
```

---

## 五、后台同步任务设计

### 5.1 TrendingRankSyncer（仿 [circle_hot_syncer.go](../pkg/server/storage/redpanda/circle_hot_syncer.go)）

```go
// pkg/server/storage/redpanda/trending_syncer.go

// TrendingRankSyncer 热点榜单定时同步器。
//
// 每 N 分钟对 3 维度(post/circle/user) × 2 窗口(24h/7d) = 6 榜单并发跑 ES 聚合，
// ZADD 覆盖重写 Redis ZSET + 更新 refreshed_at。读路径直接读 ZSET。
type TrendingRankSyncer struct {
	mu       sync.Mutex
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

var trendingSyncer *TrendingRankSyncer

func StartTrendingSyncer() error {
	interval := conf.Config.Trending.FlushIntervalMinutes  // 默认 5
	if interval <= 0 { interval = 5 }
	s := &TrendingRankSyncer{
		ticker:   time.NewTicker(time.Duration(interval) * time.Minute),
		stopChan: make(chan struct{}),
	}
	trendingSyncer = s
	go s.run()
	return nil
}

func (s *TrendingRankSyncer) run() {
	for {
		select {
		case <-s.ticker.C:
			s.flush()
		case <-s.stopChan:
			s.ticker.Stop()
			s.flush() // 关停排干
			return
		}
	}
}
```

### 5.2 flush：6 榜单并发聚合 + 覆盖写

```go
func (s *TrendingRankSyncer) flush() {
	// 6 个 (dimension, window) 组合并发执行
	type task struct{ dim, window string }
	tasks := []task{
		{"post", "24h"}, {"post", "7d"},
		{"circle", "24h"}, {"circle", "7d"},
		{"user", "24h"}, {"user", "7d"},
	}
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(t task) {
			defer wg.Done()
			s.syncOne(t.dim, t.window)
		}(t)
	}
	wg.Wait()
}

func (s *TrendingRankSyncer) syncOne(dim, window string) {
	ctx := context.Background()
	// 1. ES 聚合取 Top-N（size = top_n，如 100；上限 maxScan=500）
	items, err := elasticsearch.AggregateTrending(dim, window, conf.Config.Trending.TopN)
	if err != nil {
		logger.Log.Error("trending aggregate failed: " + err.Error())
		return // 本轮跳过，保留上轮 ZSET（降级，见 §10）
	}
	// 2. 覆盖写 ZSET
	if err := rewriteBoard(ctx, dim, window, items); err != nil {
		logger.Log.Error("trending rewrite failed: " + err.Error())
	}
}
```

> `AggregateTrending` 签名见 §8.2。
> `top_n` 默认 100（写入 ZSET 的条数，大于单次接口 `max_size=50`，给翻页留余量）。
> 并发 6 个 ES 请求量可控（每个 size:0 聚合 + filter cache 友好）。

### 5.3 启停位置（[cmd/apps/server.go](../cmd/apps/server.go)）

对照现有 syncer 启停点：

| 现有 | 位置 | 本方案新增 |
|---|---|---|
| `redis.InitHotLuaScripts` | [server.go:148](../cmd/apps/server.go#L148) | — |
| `go redpanda.StartCircleHotSyncerWithRetry()` | [server.go:164](../cmd/apps/server.go#L164) | `go redpanda.StartTrendingSyncerWithRetry()`（紧随其后） |
| `redpanda.StopCircleHotSyncer()` | [server.go:204](../cmd/apps/server.go#L204) | `redpanda.StopTrendingSyncer()`（紧随其后） |

`StartTrendingSyncerWithRetry` / `StopTrendingSyncer` 包级函数签名与 `StartCircleHotSyncerWithRetry` / `StopCircleHotSyncer`（[circle_hot_syncer.go:194,201](../pkg/server/storage/redpanda/circle_hot_syncer.go#L194)）对齐。

---

## 六、API 设计

### 6.1 路由

```
GET /trending   需登录（挂 authCheck 组）
  query: window=24h|7d  size=20  section=all|posts|circles|users  offset=0
```

注册位置：新建 `pkg/domains/trending/interfaces/http/routes.go`，挂独立 `/trending` 路由组（不挂在 `/post` 下，因 trending 不只帖子）。

### 6.2 请求参数（query）

| 参数 | 类型 | 默认 | 上限 | 说明 |
|---|---|---|---|---|
| `window` | string | `24h` | — | 时间窗，枚举 `24h` / `7d`；非法值回落 `24h` |
| `section` | string | `all` | — | 板块，枚举 `all` / `posts` / `circles` / `users`；`all`=三类各返 `size` 条（首屏聚合） |
| `size` | int | 20 | 50 | 每板块条数；`all` 时三板块各 `size` |
| `offset` | int | 0 | — | 0 基偏移，单板块翻页用；`section=all` 时忽略 offset（首屏不分页） |

> 复用 `normalizeSize` 思路（[circle handler](../pkg/domains/circle/interfaces/http/handler.go#L296)）：`size<=0||size>max` 回落默认。

### 6.3 响应 VO（预备定义于 `trending/domain/ports.go`）

```go
// TrendingPostItem 热门帖子项（镜像 recommend.FeedPostItem + 打分）。
//
// 复用 recommend.PostHydrator 回填展示字段；HotScore 来自 ZSET score。
type TrendingPostItem struct {
	// 展示字段：与 recommend.FeedPostItem 完全一致
	// （ID/CircleID/UserID/Type/Title/Summary/Content/各 Count/is_*/Author*/Circle*/Images/is_liked/is_collected）
	recommend.FeedPostItem
	HotScore float64 `json:"hot_score"` // 窗口内 hot（帖子榜=post.hot）
}

// TrendingCircleItem 热门圈子项（镜像 ActiveCircleDoc + 打分）。
type TrendingCircleItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description,omitempty"`
	CategoryID  string `json:"category_id,omitempty"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"` // 累积
	Hot         int    `json:"hot"`        // 累积
	JoinType    int16  `json:"join_type"`
	CreateTime  string `json:"create_time"`
	HotScore    float64 `json:"hot_score"` // ★ 窗口内 Σhot（趋势信号）
}

// TrendingUserItem 热门用户项（镜像 user UserBrief + 打分）。
type TrendingUserItem struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
	HotScore  float64 `json:"hot_score"` // ★ 窗口内 Σhot
}

// TrendingBoard 热点聚合看板（GET /trending 响应体）。
type TrendingBoard struct {
	Window      string             `json:"window"`        // "24h" | "7d"
	Posts       []TrendingPostItem `json:"posts,omitempty"`      // section=all|posts 时填充
	Circles     []TrendingCircleItem `json:"circles,omitempty"`  // section=all|circles 时填充
	Users       []TrendingUserItem `json:"users,omitempty"`      // section=all|users 时填充
	RefreshedAt int64              `json:"refreshed_at"`         // 榜单最近刷新 Unix 秒（0=从未刷新）
	Truncated   bool               `json:"truncated,omitempty"`  // 触达 top_n 上限（读路径无法判定时省略）
	Offset      int                `json:"offset,omitempty"`     // 单板块翻页时回显
	Size        int                `json:"size"`
}
```

字段来源说明：
- 帖子展示字段：`recommend.FeedPostItem`（[recommend/domain/ports.go:19](../pkg/domains/recommend/domain/ports.go#L19)），由 `PostHydrator` + `InteractionChecker` 回填（复用 recommend 同名端口）。
- 圈子展示字段：`circleRepo.GetByIDs`（[circle_repo_pg.go:43](../pkg/domains/circle/infrastructure/circle_repo_pg.go#L43)），返回 `map[uuid.UUID]*domain.Circle`（含 member_count/post_count/hot）。
- 用户展示字段：`UserFacade.GetBriefs`（[user/application/service.go:43](../pkg/domains/user/application/service.go#L43)），返回 `map[string]UserBrief{ID,Username,AvatarURL}`。

---

## 七、跨域 Facade 回填（只读桥接）

`trending/domain/ports.go` 定义 4 个只读端口，由 `pkg/composition/facade_bridges.go` 桥接到现有 service：

| 端口 | 桥接目标 | 复用接口 |
|---|---|---|
| `PostHydrator` | `postapp.PostService` | 与 recommend 同名端口一致（[recommend/domain/ports.go:64](../pkg/domains/recommend/domain/ports.go#L64)）：`Hydrate(ctx, postIDs) ([]FeedPostItem, error)` |
| `InteractionChecker` | recommend 同名 infra 或 post/like/collect | `BatchCheck(ctx, userID, postIDs) (liked, collected map[uuid.UUID]bool, error)`（[recommend/domain/ports.go:89](../pkg/domains/recommend/domain/ports.go#L89)） |
| `CircleFacade` | `circleapp.CircleService` 或 `circleRepo` | 新方法 `ListByIDs(ctx, ids) (map[uuid.UUID]*CircleBrief, error)`（`CircleBrief`=展示字段子集） |
| `UserFacade` | `userapp.UserFacade` | 直接复用 `GetBriefs(ctx, userIDs) (map[string]UserBrief, error)`（已存在，[user/application/service.go:43](../pkg/domains/user/application/service.go#L43)） |

### 7.1 顺序保留（关键）

ZSET `ZREVRANGE` 返回的 member 已按 score 降序。回填展示信息后**必须按原 score 序还原榜单顺序**，
否则 `GetByIDs`/`GetBriefs` 的 map 返回会打乱排名：

```go
// service 伪代码
func (s *trendingService) fillPosts(ctx, scored []ScoredID, userID uuid.UUID) []TrendingPostItem {
	ids := toUUIDs(scored)
	items, _ := s.postHydrator.Hydrate(ctx, ids)          // 顺序无关
	liked, collected, _ := s.interactionChecker.BatchCheck(ctx, userID, ids)
	byID := indexByPostID(items)                          // map[id]FeedPostItem
	result := make([]TrendingPostItem, 0, len(scored))
	for _, sc := range scored {                           // ★ 按 ZSET 序遍历，还原排名
		it, ok := byID[sc.ID]
		if !ok { continue }                               // 已删除/过滤则跳过（榜单断层，见 §10）
		it.IsLiked = liked[sc.ID]
		it.IsCollected = collected[sc.ID]
		result = append(result, TrendingPostItem{FeedPostItem: it, HotScore: sc.Score})
	}
	return result
}
```

圈子/用户板块同理（按 ZSET 序遍历，跳过 `GetByIDs`/`GetBriefs` 未命中的已删除实体）。

---

## 八、DDD 分层改动清单

### 8.1 新建 trending 领域（`pkg/domains/trending/`）

| 层 | 文件 | 职责 |
|---|---|---|
| **domain** | `domain/ports.go` | 端口接口 + DTO：`BoardStore`、`TrendingAggregator`、`PostHydrator`、`InteractionChecker`、`CircleFacade`、`UserFacade`；VO：`TrendingBoard` / `TrendingPostItem` / `TrendingCircleItem` / `TrendingUserItem` / `ScoredID` |
| **application** | `application/service.go` | `TrendingService` 接口 + 实现：`GetTrending(ctx, window, section, size, offset) (*TrendingBoard, error)`；按 section 决定读哪些 ZSET；调 Facade 回填并保序 |
| | `application/errors.go` | 领域错误（可选） |
| **infrastructure** | `infrastructure/board_store_redis.go` | `BoardStore` 实现：`Range`/`Rewrite`/`RefreshedAt`，基于 `redispkg.GetTrendingKey` |
| | `infrastructure/aggregator_es.go` | （可选）`TrendingAggregator` 实现薄封装，调 `elasticsearch.AggregateTrending`；或 service/job 直接调 ES 包 |
| **interfaces/http** | `interfaces/http/handler.go` | `GetTrending` handler + `GetTrendingRequest{Window,Section,Size,Offset}`（query 绑定 + 校验） |
| | `interfaces/http/routes.go` | `RegisterRoutes(rg, svc, authCheck)`：`rg.Group("/trending", authCheck).GET("/", h.GetTrending)` |

### 8.2 ES 层新增（[pkg/server/storage/elasticsearch/](../pkg/server/storage/elasticsearch/)）

在 `post.go`（或新 `trending_agg.go`）新增，紧邻 `AggregateActiveCircles`（[post.go:586](../pkg/server/storage/elasticsearch/post.go#L586)）：

```go
// ScoredItem 聚合产出的「实体 ID + 热度分」。
type ScoredItem struct {
	ID    string  // post_id / circle_id / user_id
	Score float64 // post 榜=post.hot；circle/user 榜=Σhot
}

// AggregateTrendingResult 趋势聚合结果。
type AggregateTrendingResult struct {
	Items     []ScoredItem
	Total     int64 // 近似活跃实体总数（cardinality，仅 circle/user 维度有意义）
	Truncated bool  // 触达 maxScan 上限
}

// AggregateTrending 热点榜聚合（3 维度 × 2 窗口统一入口）。
//   dimension = "post"   → hits 按 hot desc（§3.2）
//   dimension = "circle" → terms on circle_id + sum(hot)（§3.1）
//   dimension = "user"   → terms on user_id  + sum(hot)（§3.1）
//   window = "24h" | "7d"
//   size = 入榜条数（job 传 top_n，如 100）
// 返回有序 ScoredItem（score 降序）。
func AggregateTrending(dimension, window string, size int) (*AggregateTrendingResult, error)
```

实现要点：
- 窗口 → range `gte`：`24h`→`now-24h/h`，`7d`→`now-7d/d`。
- dimension=`post`：走 §3.2 hits 路径；其余走 §3.1 terms+sum 路径。
- 解析复用 `parseActiveCirclesResponse`（[post.go:656](../pkg/server/storage/elasticsearch/post.go#L656)）的结构，新增 `sum(hot)` 子聚合值读取（桶内 `hot_sum.value`）。

### 8.3 Redis 常量新增（[pkg/server/storage/redis/constants.go](../pkg/server/storage/redis/constants.go)）

末尾追加 §4.1 的 `TrendingPrefix` / `TrendingMetaPrefix` / `GetTrendingKey` / `GetTrendingMetaKey`。

### 8.4 redpanda 新增（[pkg/server/storage/redpanda/](../pkg/server/storage/redpanda/)）

新增 `trending_syncer.go`（§5.1–5.2 全文）。包级函数 `StartTrendingSyncer` / `StartTrendingSyncerWithRetry` / `StopTrendingSyncer`。

### 8.5 composition 装配（[pkg/composition/server.go](../pkg/composition/server.go)）

| 改动 | 位置 |
|---|---|
| 新增 `newTrendingService(postSvc, circleSvc, userSvc)` | 紧随 `newRecommendService`（[server.go:181](../pkg/composition/server.go#L181)）；直构 `BoardStore`，跨域桥接 `trendingPostHydrator`/`trendingInteractionChecker`/`trendingCircleFacade`/`trendingUserFacade` |
| `RegisterDomainRoutes` 内构造 + 注册 | 紧随 `recommendSvc`（[server.go:100](../pkg/composition/server.go#L100)）后加 `trendingSvc := newTrendingService(postSvc, circleSvc, userSvc)`；末尾（[server.go:113](../pkg/composition/server.go#L113) 后）加 `registerTrending(root, trendingSvc, authCheck)` |
| 新增桥接器 | [facade_bridges.go](../pkg/composition/facade_bridges.go)：`trendingPostHydrator`（包 postSvc）/ `trendingCircleFacade`（包 circleRepo 或 circleSvc）/ `trendingUserFacade`（包 userFacade，直接转调 `GetBriefs`） |

> `PostHydrator` / `InteractionChecker` 桥接器可与 recommend 共用同名结构体（若已存在则复用，避免重复定义）。

### 8.6 配置（[configs/config.yaml](../configs/config.yaml) + [pkg/conf/conf.go](../pkg/conf/conf.go)）

见 §九。

---

## 九、配置项

### 9.1 yaml（[configs/config.yaml](../configs/config.yaml) 新增节，仿 `hot` / `recommend` 范式）

```yaml
trending:
  flush_interval_minutes: 5   # TrendingRankSyncer 周期（分钟）
  top_n: 100                  # 每个 ZSET 榜单保留条数（> max_size 留翻页余量）
  windows: ["24h", "7d"]      # 启用的时间窗
  default_size: 20            # 接口默认 size
  max_size: 50                # 接口 size 上限
```

### 9.2 conf.go（[pkg/conf/conf.go](../pkg/conf/conf.go) 新增结构体，紧邻 `Hot`/`Recommend`）

```go
type Trending struct {
	FlushIntervalMinutes int      `mapstructure:"flush_interval_minutes"`
	TopN                 int      `mapstructure:"top_n"`
	Windows              []string `mapstructure:"windows"`
	DefaultSize          int      `mapstructure:"default_size"`
	MaxSize              int      `mapstructure:"max_size"`
}

// 顶层 Config 加字段：
type Config struct {
	...
	Trending Trending `mapstructure:"trending"`
}
```

job / service / handler 均从 `conf.Config.Trending` 读，并提供默认值兜底（`<=0` 回落常量）。

---

## 十、边界 / 一致性 / 风险（对齐 [active-circles §六](active-circles-design.md)）

| 点 | 处理 |
|---|---|
| ES mapping 前置确认 | S0 必须：`circle_id`/`user_id`=keyword、`create_time`=date、`hot`=numeric。不合规加 index template（运维，非 Go 改动） |
| 深分页排名漂移 | 趋势榜本性；offset 翻页可漂移，文档与 UI 注明，建议前端只翻前几页（对齐 active-circles §4.3） |
| Top-N 截断 | `top_n=100`；接口 `max_size=50`；超出由 `truncated` 标志提示 |
| job 与读并发 | ZSET `ZADD`/`ZREVRANGE` 原子，读到旧或新快照均可接受；`TxPipeline` 保证写的原子 |
| ES 不可用 | `syncOne` 本轮跳过（[§5.2](#五后台同步任务设计)），保留上轮 ZSET；读路径照常返回旧榜单 + `refreshed_at` 标注时效（前端显示「X 分钟前」，用户自知新鲜度）。不 5xx |
| 冷启动无数据 | ZSET 空 → 返回空板块 + `refreshed_at=0`，前端正常空态 |
| 被禁/删除实体 | 圈子：`GetByIDs` 已 `deleted=0`；`status != Normal` 应用层跳过。用户：brief 查询天然过滤。帖子：hydrate 自带 deleted/status 过滤。**跳过导致榜单断层**（保序遍历跳过未命中），与 active-circles §4.4 处理一致 |
| `hot` 字段延迟 | post.hot 经 PostHotAggregator 13min 批量落库 + CDC 同步 ES，有分钟级延迟；趋势榜本身 5min 刷新，叠加后最迟 ~18min 见效，可接受 |
| 帖子榜 vs 信息流 hot tab 重复 | 语义不同（§3.2）：信息流用 rank_score 时间衰减面向长期；本榜用窗口+原始 hot 面向「近期最热」，两接口并存不冲突 |
| 热门用户「老用户」偏差 | 窗口内 Σhot 高者即上榜，不区分新老；若产品要扶持新用户，后续可在 score 加新用户加权（属另一独立工作） |

---

## 十一、分阶段交付（对齐 [active-circles §八](active-circles-design.md)）

| 阶段 | 内容 | 验收 |
|---|---|---|
| **S0 确认 mapping** | `GET pg.public.post/_mapping` 看 `circle_id`/`user_id`/`create_time`/`hot` 类型；不合规加 index template | 字段类型合规 |
| **S1 ES 聚合** | `AggregateTrending`（3 维度 × 2 窗口）+ 解析 + 单测（mock ES 响应） | 桶按 sum(hot)/hot desc、score 解析正确、维度分发正确 |
| **S2 Redis 榜单** | `BoardStore`（`Rewrite` TxPipeline 覆盖 / `Range` ZREVRANGE / `RefreshedAt`）+ constants | 覆盖写后旧成员清空、读出有序、meta 更新 |
| **S3 同步 job** | `TrendingRankSyncer` + `server.go` 启停 + 配置 | 6 榜单周期性刷新，`refreshed_at` 滚动更新；ES 故障时保留旧榜单 |
| **S4 领域贯通** | `TrendingService.GetTrending` + 跨域 Facade 桥接 + composition 装配 | 三类榜单展示信息回填正确、**榜单顺序保留**、已删实体跳过 |
| **S5 HTTP** | handler + route `GET /trending` | `section=all` 聚合返回、单板块 + offset 翻页、`window` 切换、参数校验 |
| **S6 联调** | 真 ES/Redis 验证双窗口、排名、明细字段、降级、冷启动空态 | 端到端 |

---

## 十二、明确不做（边界）

- ❌ **WebSocket / SSE 实时推送**：选定「榜单数据刷新策略」，前端靠 `refreshed_at` + 下拉/轮询感知更新，无需长连接基建。
- ❌ **用户级热点通知 / 站内信**：需事件驱动通知系统（qubar 暂无），超出本次范围。
- ❌ **user 表新增 hot 列**：改用窗口内 Σhot 聚合，不改 schema（§2.3）。
- ❌ **新增 MQ topic**：复用现有 post CDC → ES 链路，无新事件源。
- ❌ **全天候累积榜**：用 `circle.hot`/`post.hot` 累积值会被老爆款霸榜（[active-circles §一](active-circles-design.md) 已否决），本方案坚持带时间窗。
- ❌ **实时 ES 聚合（无缓存）**：已选 job 预计算 → ZSET，避免高并发下 ES RTT 压力。

---

## 附录：与现有子系统的关系

| 子系统 | 关系 |
|---|---|
| [hot-sync-design.md](hot-sync-design.md) | **上游**：产出 `post.hot`，本方案以其为打分信号（只读消费） |
| [active-circles-design.md](active-circles-design.md) | **同族**：都是「ES 聚合做窗口化榜单」范式；本方案复用其聚合体结构、mapping 前置确认、offset 分页、`GetByIDs` 回填等模式，扩展到 3 维度 + 双窗口 + ZSET 缓存 |
| [cf-item-based-design.md](cf-item-based-design.md) | 无关（个性化召回，非全局榜单） |
| [home-feed-api.md](home-feed-api.md) | 互补：首页信息流面向个性化消费，热点页面向全局发现 |

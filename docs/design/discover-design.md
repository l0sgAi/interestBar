# 发现（Discover）领域 设计方案

> 目标：新增独立领域 `pkg/domains/discover`，对外提供 `GET /discover` 聚合接口，
> 返回**发散性随机推送的圈子 / 帖子**两个分区，支撑前端「发现」页面，帮用户探索兴趣边界。
>
> 推送机制：**反气泡候选池**——后台 job 周期性对 post/circle ES 索引做 `random_score` 随机采样，
> 排除用户已加圈子 + 已赞/藏/看帖子（登录态），写入 Redis LIST 候选池；读路径 offset 翻页。
> 匿名用户：退化为纯随机（无排除）。
>
> 设计范式与 [trending-design.md](trending-design.md) / [active-circles-design.md](active-circles-design.md) 一致。

---

## 一、目标与领域定位

### 1.1 领域定位

`discover` 是**跨域编排器（无聚合根）**，职责是「随机采样圈子+帖子 + 反气泡个性化排除 + 跨域回填展示信息」，
与 `recommend`（[pkg/domains/recommend/domain/ports.go:1](../pkg/domains/recommend/domain/ports.go#L1)）、
`trending`（[pkg/domains/trending/domain/ports.go:1](../pkg/domains/trending/domain/ports.go#L1)）同范式：

- 本包不 import `post` / `circle` / `recommend`，所有跨域依赖经 `domain/ports.go` 接口抽象，由 `pkg/composition` 注入桥接器（避免环依赖）。
- 同域 infra：候选池存储（Redis LIST）、随机采样器（ES random_score）。
- 后台同步任务（`DiscoverPoolSyncer`）放在 `pkg/server/storage/redpanda/`，与 `TrendingRankSyncer` 同层。

### 1.2 数据流（一图）

```
                    ┌──────────────────────────────────────────┐
   post ES 索引 ───▶│  DiscoverPoolSyncer（redpanda 包）        │
   circle ES 索引    │  每 10min：ES random_score 随机采样       │
   (CDC 已就位)      │  → 过滤已加圈子/已交互帖（登录态）        │
                    │  → RPUSH 写 Redis LIST 候选池 + 版本 token │
                    └────────────────────┬─────────────────────┘
                                         │ 写（登录用户独立池；匿名共享全局池）
                                         ▼
   ┌──────────────────────────────────────────────────────────┐
   │  Redis LIST 候选池                                        │
   │  discover:posts:{uid}      LIST (登录态，反气泡排除后)    │
   │  discover:circles:{uid}    LIST                           │
   │  discover:anon:posts       LIST (匿名全局共享，纯随机)    │
   │  discover:anon:circles     LIST                           │
   │  discover:token:{uid}      string (版本 token)            │
   └────────────────────┬─────────────────────────────────────┘
                        │ 读（LRANGE offset）
                        ▼
   ┌──────────────────────────────────────────────────────────┐
   │  DiscoverService.GetDiscover                              │
   │  LIST 拿 id → 校验 pool_token → 跨域 Facade 回填展示信息  │
   │  → 组装 DiscoverBoard（circles + posts 两个分区）         │
   └────────────────────┬─────────────────────────────────────┘
                        │
                        ▼
              GET /discover?size=20&offset=0&pool_token=...
```

### 1.3 为什么是独立领域（不复用 recommend / trending）

| 维度 | recommend | trending | **discover** |
|---|---|---|---|
| 服务对象 | 首页信息流（个性化召回） | 热点页（全局榜单） | 发现页（发散探索） |
| 召回信号 | hot/CF/相似（**收敛**） | hot（全局收敛） | **random_score（发散）** |
| 个性化 | 兴趣召回（推你爱看的） | 无（全局同榜） | **反气泡排除（推你没看过的）** |
| 数据形态 | 帖子（单一） | 圈子+帖子+用户 | 圈子+帖子（两分区） |
| 读路径 | 候选池 LIST offset | ZSET ZREVRANGE | 候选池 LIST offset |
| 写路径 | miss 时 5 路召回重建 | 后台 job 周期覆盖 | 后台 job 周期重建 |
| 匿名 | 禁（401） | 禁（401） | **允许（纯随机退化）** |

三者职责正交：recommend 收敛推荐、trending 全局热点、**discover 发散探索**。塞进任一都会扭曲其语义，故独立成域。
三者仍可共享只读跨域端口（`PostHydrator` / `InteractionChecker` / `CircleLookup`）。

---

## 二、数据源与打分信号

### 2.1 数据源：post / circle ES 索引

`PostDocument`（[pkg/server/storage/elasticsearch/post.go:13](../pkg/server/storage/elasticsearch/post.go#L13)）：
`ID` / `CircleID` / `UserID` / `Hot` / `CreateTime` / `Deleted` / `Status` 全备。
`CircleDocument`（[pkg/server/storage/elasticsearch/circle.go:13](../pkg/server/storage/elasticsearch/circle.go#L13)）：
`ID` / `Name` / `CategoryID` / `Hot` / `MemberCount` / `PostCount` / `Status` / `Deleted` / `JoinType` 全备。

### 2.2 打分信号：random_score（核心，发散来源）

```jsonc
// 帖子随机采样
GET pg.public.post/_search
{
  "size": 200,
  "query": {
    "function_score": {
      "query": {
        "bool": {
          "filter": [
            { "term": { "deleted": 0 } },
            { "term": { "status": 1 } },
            { "bool": { "must_not": [{ "terms": { "circle_id": [已加圈子...] } }] } }
          ]
        }
      },
      "functions": [{ "random_score": {} }],
      "score_mode": "sum"
    }
  },
  "_source": ["id"]
}
```

- `random_score`：每次查询打乱排序（无 seed，每次采样结果不同——发散性来源）。
- `filter`（非 must）：纯结构化过滤，命中 ES request cache。
- `must_not terms circle_id`：**反气泡排除已加圈子**（登录态；匿名省略此 clause）。
- 帖子排除已交互（liked/collected/viewed）在 syncer 内做（见 §5.3，Redis ZSET 已有数据，比 ES terms filter 高效、避免子句数 1024 上限）。
- 圈子采样同理，索引换 circle、`must_not terms id = 已加圈子`、附加过滤 `join_type != 2`（排除私圈）+ `status=1`。

### 2.3 为什么用 random_score 而非 hot / rank_score

| 信号 | 语义 | 选用 |
|---|---|---|
| `random_score` | 纯随机发散，主动打破过滤气泡 | ✅ 发现页本质 |
| `hot` / `rank_score` | 热度收敛（recommend/trending 已用） | ❌ 与发现语义相反 |
| CF 相似 | 兴趣收敛（recommend 已用） | ❌ |

**关键**：random_score 是发散的「主信号」，反气泡排除是「边界约束」——只排除已见，不引入兴趣偏向，保证用户看到的是真正陌生的新内容。

---

## 三、ES 查询设计

### 3.1 帖子随机采样（job 阶段）

```go
// SampleDiscoverPosts 发现页帖子随机采样。
//   excludeCircleIDs：登录用户已加圈子（反气泡排除）；nil/空=全局采样（匿名）
//   size：采样数量（job 传 pool_size，如 200）
// 返回随机 postID 列表。
func SampleDiscoverPosts(excludeCircleIDs []uuid.UUID, size int) ([]string, error)
```

实现要点（对照 [post.go:711 SearchHomeFeed](../pkg/server/storage/elasticsearch/post.go#L711)）：
- 基础过滤同 SearchHomeFeed：`deleted=0`、`status=1`。
- `excludeCircleIDs` 非空 → 追加 `must_not terms circle_id`。
- 用 `function_score` + `random_score` 替换 rank_score runtime mapping。
- `size` 归一化（`<=0 || >500` 回 200）。
- `_source: ["id"]` 只取 id，省传输。

### 3.2 圈子随机采样（job 阶段）

```go
// SampleDiscoverCircles 发现页圈子随机采样。
//   excludeCircleIDs：已加圈子（反气泡）；nil=全局（匿名）
//   size：采样数量
func SampleDiscoverCircles(excludeCircleIDs []uuid.UUID, size int) ([]string, error)
```

实现要点（对照 [circle.go:44 SearchCircles](../pkg/server/storage/elasticsearch/circle.go#L44)）：
- 基础过滤：`status=1`、`deleted=0`、`must_not join_type=2`（与 SearchCircles 一致，排除私圈）。
- `excludeCircleIDs` 非空 → `must_not terms id`。
- 同样 `function_score` + `random_score`。

### 3.3 前置确认（S0）

`random_score` 是 ES 内置 function，对 mapping 无额外要求（不需 seed 字段）。
仅确认 `circle_id` / `status` / `deleted` / `join_type` 为 keyword/numeric（与 SearchHomeFeed / SearchCircles 已假定一致，复用其假设）。

---

## 四、Redis 候选池存储设计

### 4.1 Key 规范（将加入 [constants.go](../pkg/server/storage/redis/constants.go)）

```go
// DiscoverPoolPrefix 发现页候选池 key 前缀（登录用户独立池）。
// 完整 key 格式: discover:{posts|circles}:{user_id}
// LIST: member=实体 ID(uuid 字符串)，RPUSH 写入，LRANGE offset 翻页。
// 由 DiscoverPoolSyncer 周期性 RPUSH 写入（先 DEL 再 RPUSH 重建）；TTL ttl_minutes（默认 30）。
const DiscoverPoolPrefix = "discover:"

// DiscoverAnonPrefix 发现页匿名共享池前缀。
// 完整 key: discover:anon:{posts|circles}
// 纯随机无排除，所有匿名用户共享；TTL 同登录池。
const DiscoverAnonPrefix = "discover:anon:"

// DiscoverTokenPrefix 发现页候选池版本 token 前缀。
// 完整 key: discover:token:{user_id}（登录）/ discover:token:anon（匿名）。
// 客户端回传 pool_token；不匹配=池已重建，回 offset=0 + pool_refreshed=true。
const DiscoverTokenPrefix = "discover:token:"

func GetDiscoverPoolKey(section, userID string) string { return DiscoverPoolPrefix + section + ":" + userID }
func GetDiscoverAnonKey(section string) string          { return DiscoverAnonPrefix + section }
func GetDiscoverTokenKey(userID string) string          { return DiscoverTokenPrefix + userID }
```

### 4.2 Key 表

| key | 类型 | member | 语义 | TTL |
|---|---|---|---|---|
| `discover:posts:{uid}` | LIST | post_id | 登录用户反气泡排除后随机帖池 | 30min(cfg) |
| `discover:circles:{uid}` | LIST | circle_id | 登录用户反气泡排除后随机圈池 | 30min(cfg) |
| `discover:anon:posts` | LIST | post_id | 匿名全局随机帖池 | 30min(cfg) |
| `discover:anon:circles` | LIST | circle_id | 匿名全局随机圈池 | 30min(cfg) |
| `discover:token:{uid}` | string | 版本 token | 池版本（登录，每 uid 独立） | 30min(cfg) |
| `discover:token:anon` | string | 版本 token | 匿名池版本 | 30min(cfg) |

### 4.3 写入策略：DEL + RPUSH 重建（job 阶段）

```go
// PoolStore.Rebuild 伪代码（infrastructure/pool_store_redis.go）
func (s *poolStoreRedis) Rebuild(ctx, section, userKey string, ids []uuid.UUID, ttl time.Duration) (string, error) {
    key := redispkg.GetDiscoverPoolKey(section, userKey)
    token := uuid.NewString()
    pipe := s.client.TxPipeline()
    pipe.Del(ctx, key)                                  // 先清空（重建语义）
    if len(ids) > 0 {
        values := make([]interface{}, len(ids))
        for i, id := range ids { values[i] = id.String() }
        pipe.RPush(ctx, key, values...)                 // 批量写入
        pipe.Expire(ctx, key, ttl)
    }
    pipe.Set(ctx, redispkg.GetDiscoverTokenKey(userKey), token, ttl)
    _, err := pipe.Exec(ctx)
    return token, err                                   // 返回新 token 供回传客户端
}
```

> 为什么 `DEL + RPUSH` 而非 `RPUSH + LTRIM`：发现池是「每次全量随机重建」，非增量；DEL+RPUSH 一步到位。
> 与 recommend `FeedCache.Build`（[recommend/infrastructure](../pkg/domains/recommend/infrastructure/)）同构——复用 LIST 池模式。
> token 用 uuid（与 recommend token 风格一致）。
> `TxPipeline` 保证写的原子；job 并发只有一个（mu 互斥 + 单 goroutine），无竞态。

### 4.4 读策略（service 阶段）

```go
// PoolStore LRANGE offset 偏移取；Token 校验版本；Exists/Len 判断池存在与 HasMore。
type DiscoverPoolStore interface {
    Range(ctx context.Context, section, userKey string, offset, size int64) ([]uuid.UUID, error)
    Token(ctx context.Context, userKey string) (string, error)
    Exists(ctx context.Context, section, userKey string) (bool, error)
    Len(ctx context.Context, section, userKey string) (int64, error)
}
```

---

## 五、后台同步任务设计

### 5.1 两种重建时机（读路径 miss 重建 + 后台周期刷新）

发现池有两种重建时机：
- **读路径 miss 重建（同步）**：用户首次访问 / token 不匹配 → 触发一次重建（同 recommend miss 模式，保证响应不空）。
- **后台周期刷新（异步）**：`DiscoverPoolSyncer` 每 N 分钟重建「匿名共享池」（必重建）+「近期活跃登录用户池」，让内容保鲜（「换一批」感）。

> 为避免后台 syncer 扫描全部用户（无法实现），syncer 仅重建**匿名共享池**（`discover:anon:*`，必重建）+
> **热点登录用户池**（通过 SCAN 近期 token 提取活跃用户集合，见 §5.2）。
> 大多数登录用户的池由「读路径 miss 重建」懒触发。

### 5.2 DiscoverPoolSyncer（仿 [trending_syncer.go](../pkg/server/storage/redpanda/trending_syncer.go)）

```go
// pkg/server/storage/redpanda/discover_syncer.go

// DiscoverPoolSyncer 发现页候选池定时刷新器。
//
// 每 N 分钟：① 重建匿名共享池（纯随机）；② 扫描近期活跃登录用户 token，
// 对仍在 TTL 内的用户重建其反气泡池（内容保鲜）。读路径 miss 时也会同步重建。
type DiscoverPoolSyncer struct {
    mu       sync.Mutex
    ticker   *time.Ticker
    stopChan chan struct{}
    stopped  bool
}
var discoverSyncer *DiscoverPoolSyncer

func StartDiscoverSyncer() error {
    interval := conf.Config.Discover.RefreshIntervalMinutes  // 默认 10
    if interval <= 0 { interval = 10 }
    s := &DiscoverPoolSyncer{
        ticker:   time.NewTicker(time.Duration(interval) * time.Minute),
        stopChan: make(chan struct{}),
    }
    discoverSyncer = s
    go s.run()
    logger.Log.Info(fmt.Sprintf("Discover pool syncer started (interval=%d min)", interval))
    return nil
}

func (s *DiscoverPoolSyncer) flush() {
    ctx := context.Background()
    // 1. 匿名共享池（必重建）
    s.rebuildAnon(ctx)
    // 2. 近期活跃登录用户池（SCAN discover:token:* 提取 uid，TTL 内=近期活跃）
    activeUIDs := s.scanActiveUsers(ctx)  // SCAN discover:token:* → 解析 uid
    for _, uid := range activeUIDs {
        s.rebuildForUser(ctx, uid)
    }
}
```

### 5.3 rebuildForUser：反气泡重建（读路径 miss 与 syncer 共用）

```go
func (s *DiscoverPoolSyncer) rebuildForUser(ctx context.Context, uid uuid.UUID) {
    // 1. 取排除集（已加圈子 + 已交互帖）
    joinedCircles, _ := circleLookup.ListJoinedCircleIDs(ctx, uid, 100)        // 复用 recommend.CircleLookup 桥接
    liked, _ := seedReader.LikedPostIDs(ctx, uid, 500)                          // 复用 recommend.SeedReader
    collected, _ := seedReader.CollectedPostIDs(ctx, uid, 500)
    viewed, _ := seedReader.ViewedPostIDs(ctx, uid, 500)
    interactedSet := union(liked, collected, viewed)

    // 2. ES 随机采样（圈子排除已加；帖子排除已加圈子，已交互帖在内存再过滤）
    circleIDs, _ := elasticsearch.SampleDiscoverCircles(joinedCircles, poolSize)
    rawPostIDs, _ := elasticsearch.SampleDiscoverPosts(joinedCircles, poolSize*2)  // 多取以补剔除

    // 3. 内存剔除已交互帖
    postIDs := filterOutStrings(rawPostIDs, toStringSet(interactedSet))
    // 兜底：剔除后过少 → 不排除再采一次（保证非空）
    if len(postIDs) < minPoolPosts {
        postIDs, _ = elasticsearch.SampleDiscoverPosts(joinedCircles, poolSize)  // 不剔除
    }

    // 4. 重建 LIST 池
    poolStore.Rebuild(ctx, "circles", uid.String(), circleIDs, ttl)
    poolStore.Rebuild(ctx, "posts", uid.String(), postIDs, ttl)
}
```

> **为什么帖子已交互在内存过滤而非 ES terms filter**：liked/collected/viewed 总量可能上千，
> `terms` 查询子句有上限（默认 1024）且性能随子句数下降；已有 Redis ZSET 数据，内存 set 过滤更稳。
> 圈子已加数量可控（通常 <100），用 ES terms filter 高效。

### 5.4 启停位置（[cmd/apps/server.go](../cmd/apps/server.go)）

| 现有 | 位置 | 本方案新增 |
|---|---|---|
| `go redpanda.StartTrendingSyncerWithRetry()` | [server.go:167](../cmd/apps/server.go#L167) | `go redpanda.StartDiscoverSyncerWithRetry()`（紧随其后） |
| `redpanda.StopTrendingSyncer()` | [server.go:209](../cmd/apps/server.go#L209) | `redpanda.StopDiscoverSyncer()`（紧随其后） |

`StartDiscoverSyncerWithRetry` / `StopDiscoverSyncer` 包级函数签名与 `StartTrendingSyncerWithRetry` / `StopTrendingSyncer`（[trending_syncer.go](../pkg/server/storage/redpanda/trending_syncer.go)）对齐。

---

## 六、API 设计

### 6.1 路由

```
GET /discover   允许匿名（挂 authCheck 组但 handler 用 requireUserIDAllowAnon）
  query: size=20  offset=0  pool_token=...  section=all|posts|circles
```

注册位置：新建 `pkg/domains/discover/interfaces/http/routes.go`，挂独立 `/discover` 路由组（不挂 `/post` 下，因 discover 含圈子）。

### 6.2 请求参数（query，用 `query:` tag）

| 参数 | 类型 | 默认 | 上限 | 说明 |
|---|---|---|---|---|
| `size` | int | 20 | 50 | 每分区条数；`all` 时两分区各 `size` |
| `offset` | int | 0 | — | 0 基偏移，单分区翻页；`section=all` 忽略 |
| `pool_token` | string | — | — | 客户端回传的池版本；不匹配→池重建+回 offset=0+`pool_refreshed=true` |
| `section` | string | `all` | — | 枚举 `all` / `posts` / `circles`；`all`=两分区各返 size（首屏） |

> 复用 `normalizeSize`（`<=0 || >max` 回默认），与 trending 一致。

### 6.3 响应 VO（`discover/domain/ports.go`）

```go
// DiscoverPostItem 发现页帖子项（复用 recommend.FeedPostItem）。
type DiscoverPostItem struct {
    recommenddomain.FeedPostItem  // 嵌入（sanctioned DTO 复用，同 trending/domain/ports.go:7-9 范式）
}

// DiscoverCircleItem 发现页圈子项（镜像 trending.TrendingCircleItem 去掉 HotScore）。
type DiscoverCircleItem struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    AvatarURL   string `json:"avatar_url,omitempty"`
    Description string `json:"description,omitempty"`
    CategoryID  string `json:"category_id,omitempty"`
    MemberCount int    `json:"member_count"`
    PostCount   int    `json:"post_count"`
    Hot         int    `json:"hot"`
    JoinType    int16  `json:"join_type"`
    CreateTime  string `json:"create_time"`
}

// DiscoverBoard 发现页聚合看板（GET /discover 响应体）。
type DiscoverBoard struct {
    Circles       []DiscoverCircleItem `json:"circles,omitempty"`
    Posts         []DiscoverPostItem   `json:"posts,omitempty"`
    PoolToken     string               `json:"pool_token,omitempty"`
    HasMore       bool                 `json:"has_more"`
    PoolRefreshed bool                 `json:"pool_refreshed,omitempty"`
    Offset        int                  `json:"offset,omitempty"`
    Size          int                  `json:"size"`
}
```

字段来源（复用现有跨域端口）：
- 帖子：`recommend.FeedPostItem`（[recommend/domain/ports.go:19](../pkg/domains/recommend/domain/ports.go#L19)）由 `PostHydrator` + `InteractionChecker` 回填（复用 recommend 同名端口）。
- 圈子：`circleRepo.GetByIDs`（[circle/domain/repository.go:18](../pkg/domains/circle/domain/repository.go#L18)）返回 `*Circle` 实体含全部字段。

---

## 七、跨域 Facade 回填（只读桥接）

`discover/domain/ports.go` 定义只读端口，由 `pkg/composition/facade_bridges.go` 桥接（与 recommend/trending 同构桥接器）：

| 端口 | 桥接目标 | 复用接口 |
|---|---|---|
| `PostHydrator` | `postapp.PostService` | 与 recommend 同名端口一致：`Hydrate(ctx, postIDs) ([]FeedPostItem, error)` |
| `InteractionChecker` | `redispkg.BatchCheckPostLiked/Collected` | 与 trending 同名桥接一致（stateless） |
| `CircleLookup` | `circleRepo.GetByIDs` | 与 trending 同名桥接一致：返回 `map[uuid.UUID]*Circle` |
| `SeedReader` | `redispkg` user:like/collect/view ZSET | 与 recommend 同名端口一致（反气泡排除用） |
| `JoinedCircleLookup` | `circleSvc.ListJoinedCircleIDs` | 与 recommend.CircleLookup 一致（反气泡排除已加圈子用） |

### 7.1 顺序保留（同 trending §7.1）

LRANGE 返回按 LIST 写入序（随机采样序）。回填展示信息后**必须按原序还原**，否则 GetByIDs/Hydrate 的 map 返回会打乱顺序：

```go
// service 伪代码（仿 trending fillPosts）
func (s *discoverService) fillPosts(ctx, ids []uuid.UUID, userID *uuid.UUID) []DiscoverPostItem {
    items, _ := s.postHydrator.Hydrate(ctx, ids)
    byID := indexByPostID(items)
    var liked, collected map[uuid.UUID]bool
    if userID != nil {                                      // 登录态回填交互
        liked, collected, _ = s.interactionChecker.BatchCheck(ctx, *userID, ids)
    }
    var result []DiscoverPostItem
    for _, id := range ids {                                // ★ 按 LIST 序遍历，保随机序
        if it, ok := byID[id]; ok {
            if userID != nil {
                it.IsLiked, it.IsCollected = liked[id], collected[id]
            }
            result = append(result, DiscoverPostItem{FeedPostItem: it})
        }                                                   // 已删除跳过（池断层）
    }
    return result
}
```

### 7.2 读路径 miss 重建（保证响应不空）

```go
func (s *discoverService) GetDiscover(ctx, userID *uuid.UUID, section string, size, offset int, poolToken string) (*DiscoverBoard, error) {
    userKey := "anon"                                   // 匿名
    if userID != nil { userKey = userID.String() }      // 登录

    // 池不存在 OR token 不匹配 → 重建（登录态反气泡；匿名纯随机）
    exists, _ := s.poolStore.Exists(ctx, "posts", userKey)
    curToken, _ := s.poolStore.Token(ctx, userKey)
    poolRefreshed := false
    if !exists || (poolToken != "" && poolToken != curToken) {
        s.RebuildPool(ctx, userID)                      // syncer 同款逻辑抽公共方法（§5.3）
        poolRefreshed = true
        offset = 0                                      // 重建回 0
    }
    // LRANGE 取 id → Facade 回填 → 组装 board
    ...
}
```

> **与 recommend 的关键差异**：recommend 候选池 miss 时在 service 内做 5 路 ES 召回（重）；
> discover 重建只需 1-2 次 random_score 采样（轻），故同步重建可接受。后台 syncer 负责保鲜，降低重建频率。

---

## 八、DDD 分层改动清单

### 8.1 新建 discover 领域（`pkg/domains/discover/`）

| 层 | 文件 | 职责 |
|---|---|---|
| **domain** | `domain/ports.go` | 端口接口 + DTO：`DiscoverPoolStore`、`PostHydrator`、`InteractionChecker`、`CircleLookup`、`SeedReader`、`JoinedCircleLookup`；VO：`DiscoverBoard` / `DiscoverPostItem` / `DiscoverCircleItem`；哨兵 `ErrInvalidSection` |
| **application** | `application/service.go` | `DiscoverService` 接口 + 实现：`GetDiscover(ctx, userID *uuid.UUID, section, size, offset, poolToken) (*DiscoverBoard, error)`；`RebuildPool(ctx, userID)`（读路径 miss + syncer 共用） |
| | `application/errors.go` | `errSection` + `IsSectionErr` 谓词 |
| **infrastructure** | `infrastructure/pool_store_redis.go` | `DiscoverPoolStore` 实现：`Range` / `Token` / `Exists` / `Len` / `Rebuild`，基于 redispkg.GetDiscoverXxxKey |
| **interfaces/http** | `interfaces/http/handler.go` | `GetDiscover` handler + `GetDiscoverRequest{Size,Offset,PoolToken,Section}`（query 绑定 + normalizeSize）；用 `requireUserIDAllowAnon` |
| | `interfaces/http/routes.go` | `RegisterRoutes(rg, svc, authCheck)`：`rg.Group("/discover", authCheck).GET("/", h.GetDiscover)` |

> `userID *uuid.UUID`（可空）贯穿 service，匿名时传 nil；handler 用 `requireUserIDAllowAnon` 取（参考 [recommend handler requireUserID](../pkg/domains/recommend/interfaces/http/handler.go#L67) vs 匿名版）。

### 8.2 ES 层新增（[pkg/server/storage/elasticsearch/](../pkg/server/storage/elasticsearch/)）

新增 `discover.go`（或加 post.go / circle.go 末尾）：

```go
// SampleDiscoverPosts 发现页帖子随机采样（random_score）。
//   excludeCircleIDs：已加圈子（反气泡）；nil=全局（匿名）
//   size：采样数（job/读路径重建传 pool_size）
func SampleDiscoverPosts(excludeCircleIDs []uuid.UUID, size int) ([]string, error)

// SampleDiscoverCircles 发现页圈子随机采样（random_score）。
//   excludeCircleIDs：已加圈子；nil=全局
//   size：采样数
func SampleDiscoverCircles(excludeCircleIDs []uuid.UUID, size int) ([]string, error)
```

实现紧邻 [post.go:711 SearchHomeFeed](../pkg/server/storage/elasticsearch/post.go#L711) / [circle.go:44 SearchCircles](../pkg/server/storage/elasticsearch/circle.go#L44)，复用其过滤结构，仅把 sort 换成 function_score + random_score。

### 8.3 Redis 常量新增（[constants.go](../pkg/server/storage/redis/constants.go)）

末尾追加 §4.1 的 `DiscoverPoolPrefix` / `DiscoverAnonPrefix` / `DiscoverTokenPrefix` + 3 个 `GetDiscoverXxxKey` helper。

### 8.4 redpanda 新增（[pkg/server/storage/redpanda/](../pkg/server/storage/redpanda/)）

新增 `discover_syncer.go`（§5 全文）。`DiscoverPoolSyncer` 需注入跨域端口（CircleLookup / SeedReader）——
采用 syncer 构造时接收已构造的 recommend 桥接器（或独立构造轻量桥接器），避免重复。
包级 `StartDiscoverSyncer` / `StartDiscoverSyncerWithRetry` / `StopDiscoverSyncer`。

### 8.5 composition 装配（[server.go](../pkg/composition/server.go)）

| 改动 | 位置 |
|---|---|
| `newDiscoverService(postSvc, circleRepo, circleSvc, userFacade)` | 紧随 `newTrendingService`（[server.go:210](../pkg/composition/server.go#L210)）；直构 `DiscoverPoolStore`，跨域桥接复用 recommend/trending 同名桥接器 |
| `RegisterDomainRoutes` 内构造 + 注册 | 紧随 `trendingSvc`（[server.go:106](../pkg/composition/server.go#L106)）后 `discoverSvc := newDiscoverService(...)`；末尾加 `registerDiscover(root, discoverSvc, authCheck)` |
| syncer 启停传参 | syncer 需跨域端口 → 在 server.go 启动处注入已构造桥接器（与 `newDiscoverService` 共用桥接器实例） |
| 新增桥接器（如需） | facade_bridges.go：复用 `recommendCircleLookup` / `recommendSeedReader` / `recommendPostHydrator` / `trendingCircleLookup` / `trendingInteractionChecker`，加编译期 guard `var _ port = (*bridge)(nil)`（采 trending 风格，补 recommend 缺失的 guard） |

### 8.6 配置（[config.yaml](../configs/config.yaml) + [conf.go](../pkg/conf/conf.go)）

见 §九。

---

## 九、配置项

### 9.1 yaml（[configs/config.yaml](../configs/config.yaml) 新增节，仿 trending）

```yaml
discover:
  refresh_interval_minutes: 10   # DiscoverPoolSyncer 周期（分钟）
  pool_size: 200                 # 每分区候选池大小（采样数）
  min_pool_posts: 50             # 反气泡剔除后帖子最少保留数（不足则不排除重采）
  ttl_minutes: 30                # 候选池 + token TTL（分钟）
  default_size: 20               # 接口默认 size
  max_size: 50                   # 接口 size 上限
  seed_limit: 500                # 反气泡读取已交互帖上限（liked/collected/viewed 各）
```

### 9.2 conf.go（紧邻 Trending，[conf.go:225](../pkg/conf/conf.go#L225)）

```go
type Discover struct {
    RefreshIntervalMinutes int `mapstructure:"refresh_interval_minutes" json:"refresh_interval_minutes" yaml:"refresh_interval_minutes"`
    PoolSize               int `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size"`
    MinPoolPosts           int `mapstructure:"min_pool_posts" json:"min_pool_posts" yaml:"min_pool_posts"`
    TTLMinutes             int `mapstructure:"ttl_minutes" json:"ttl_minutes" yaml:"ttl_minutes"`
    DefaultSize            int `mapstructure:"default_size" json:"default_size" yaml:"default_size"`
    MaxSize                int `mapstructure:"max_size" json:"max_size" yaml:"max_size"`
    SeedLimit              int `mapstructure:"seed_limit" json:"seed_limit" yaml:"seed_limit"`
}

// 顶层 Config 加字段：
type Config struct {
    ...
    Discover Discover `mapstructure:"discover"`
}
```

job / service / handler 均从 `conf.Config.Discover` 读，并提供默认值兜底（`<=0` 回落常量）。

---

## 十、边界 / 一致性 / 风险

| 点 | 处理 |
|---|---|
| 随机采样重复 | random_score 单次查询内不重复（ES 保证）；跨池重建自然换批；分页去重靠池内 ID 唯一 |
| 翻页漂移 | 池是 LIST 快照，翻页期间不重建（token 不变），offset 切片稳定；仅后台刷新/miss 重建时换池（token 变→回 offset=0 + pool_refreshed=true 提示） |
| 反气泡过滤后池过空 | 兜底：剔除后 < min_pool_posts → 不排除再采（§5.3），保证非空；匿名无排除永不空 |
| ES 不可用 | syncer.rebuild 跳过保留旧池（降级）；读路径 miss 重建失败→回空 board（不 5xx，前端空态）；与 trending §10 降级一致 |
| 冷启动 | 池不存在→读路径首次访问同步重建（§7.2）；匿名池由 syncer 启动即建 |
| 后台扫活跃用户成本 | SCAN discover:token:*（TTL 内=活跃），数量可控；SYNC 间隔 10min，ES 采样轻（function_score），并发可控 |
| 已删/被禁实体 | GetByIDs/Hydrate 自带 deleted/status 过滤；跳过致池断层（保序遍历跳过，同 trending） |
| 同步重建延迟 | discover 重建仅 1-2 次 random_score（轻于 recommend 5 路召回），首次访问延迟可接受（<500ms） |
| 帖子无 category 维度 | 反气泡仅靠圈子+交互排除，不需 post.category；不扩 ES 索引（v1 不做类目探索） |
| 私圈泄露 | SampleDiscoverCircles `must_not join_type=2` + circle GetByIDs 自带 status 过滤；展示不暴露私圈内容 |

---

## 十一、分阶段交付

| 阶段 | 内容 | 验收 |
|---|---|---|
| **S0 确认 mapping** | 确认 circle_id / id / status / deleted / join_type 类型（复用 SearchHomeFeed / SearchCircles 假设） | 字段类型合规 |
| **S1 ES 采样** | `SampleDiscoverPosts` / `SampleDiscoverCircles`（random_score）+ 解析 + 单测 | 随机返回、exclude 过滤生效、size 归一 |
| **S2 Redis 池** | `DiscoverPoolStore`（Rebuild TxPipeline / Range LRANGE / Token / Exists / Len）+ constants | 重建后池更新、offset 切片、token 匹配判定 |
| **S3 读路径 miss 重建** | `DiscoverService.RebuildPool` + `GetDiscover`（含 miss 同步重建） | 登录反气泡、匿名纯随机、token 不匹配回 offset=0 |
| **S4 syncer** | `DiscoverPoolSyncer`（匿名池必重建 + 活跃用户池周期刷新）+ server.go 启停 + 配置 | 周期刷新、ES 故障降级保留旧池 |
| **S5 跨域 Facade + composition** | 桥接器（复用 recommend/trending 同名）+ 编译期 guard + 装配 | 展示字段回填、随机序保留、已删跳过 |
| **S6 HTTP** | handler + route `GET /discover`（匿名用 requireUserIDAllowAnon） | section=all 聚合、分页、pool_token、匿名/登录分支 |
| **S7 联调** | 真 ES/Redis 验证随机性、反气泡、匿名退化、降级、空态 | 端到端 |

---

## 十二、明确不做（边界）

- ❌ **WebSocket / SSE 实时推送**：候选池 + 客户端 pool_token + 下拉刷新，无需长连接。
- ❌ **类目探索 / post 加 category 维度**：v1 反气泡仅靠圈子+交互排除；类目 tab 属 P2（需扩 post ES 索引）。
- ❌ **混合交错流**：选分区返回（circles + posts 独立），不做 interleave（前端渲染复杂、分页难）。
- ❌ **用户榜**：trending 已覆盖热门用户；发现页聚焦圈子+帖子。
- ❌ **CF/相似召回**：那是 recommend 收敛语义；发现页坚持 random_score 发散。
- ❌ **扩 ES 索引 schema**：random_score 对 mapping 无要求，复用现有索引。

---

## 附录：与现有子系统的关系

| 子系统 | 关系 |
|---|---|
| [recommend](../pkg/domains/recommend/) | **同族**：同 LIST 候选池 + offset 翻页 + 跨域 Facade 范式；discover 复用其 PostHydrator / SeedReader / CircleLookup 桥接器与 FeedPostItem DTO |
| [trending-design.md](trending-design.md) | **同族**：同后台 syncer + Redis 缓存 + 跨域回填保序范式；discover 复用其 syncer 结构、CircleLookup 桥接、fillPosts 保序逻辑 |
| [home-feed-api.md](home-feed-api.md) | 互补：首页信息流=个性化消费；发现页=发散探索 |
| [hot-sync-design.md](hot-sync-design.md) | 无直接依赖（发现页不用 hot 打分，用 random_score） |

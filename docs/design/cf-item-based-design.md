# Item-based 协同过滤推荐流 设计文档

> 范围：为首页「推荐」tab 新增一条 **协同过滤召回（C5）**，基于 item-based 共现相似度。
> 不含推荐流本体（C1~C4 多路召回 + 交错合并）的设计，那部分见首页推荐流设计；本文只覆盖 CF 这一条召回路及其依赖。
> 不含 user-based / MF（矩阵分解），那是后续阶段。

---

## 一、背景与目标

推荐流目前只有「热门 / 圈子 / 最新」这类非个性化召回。要做个性化，最低成本、可解释、零 ML 依赖的方案是 **item-based 协同过滤**：

- 不分析帖子内容，只利用「群体的行为相似性」
- 直觉：很多同时点赞过帖子 A 的人也点赞过 B → A、B 相似 → 用户赞过 A 就推 B
- 物品相似度稳定（帖子身份不变）、可缓存、#帖子 ≪ #用户 → 适合先做

**目标**：用户进推荐 tab 时，基于他历史点赞/收藏的帖子，召回「相似帖子」作为一条独立召回路 C5，并入多路召回合并排序。

---

## 二、现状盘点：交互矩阵数据源

CF 的地基是「用户 × 帖子」交互矩阵。qubar 已有 5 张交互事实表，**零补采**：

| 表 | 文件 | 交互 | 权重 |
|---|---|---|---|
| `domains.post_like` | [post.go:99](../pkg/domains/post/domain/post.go#L99) | 点赞 | 3 |
| `domains.post_collect` | [collect.go:15](../pkg/domains/collect/domain/collect.go#L15) | 收藏（最强） | 5 |
| `domains.comment` | [comment.go:16](../pkg/domains/comment/domain/comment.go#L16) | 评论 | 4 |
| `domains.comment_like` | [comment.go:47](../pkg/domains/comment/domain/comment.go#L47) | 评论点赞（冗余 post_id） | 2 |
| `domains.post_view_history` | [history repo](../pkg/domains/history/domain/repository.go#L13) | 浏览（隐式弱） | 1 |

问题：数据分散在 5 张表，无统一评分视图。CF 需要一张 `r(user, post)` 矩阵 → 需物化 `domains.post_interaction`。

---

## 三、总体架构与数据流

```
┌──────────────────────────────────────────────────────────────────────┐
│ 写路径（事件驱动双写，复用现有 publisher 肌肉）                          │
│                                                                      │
│  like/collect/comment/comment_like/view                              │
│    └─ 各自 event publisher 额外发一条 interaction 事件                  │
│         → Redpanda topic: post_interaction                           │
│              → InteractionConsumer 批量 upsert                        │
│                   → domains.post_interaction (user_id,post_id,weight,ts)│
└──────────────────────────────────────────────────────────────────────┘
                                      │
                                      │ 离线（夜间，不在请求路径）
                                      ▼
┌──────────────────────────────────────────────────────────────────────┐
│ ItemCFSyncer（参照 CircleHotSyncer 套路）                              │
│   1. 取候选帖子集 C（近 30 天 + 已发布）                                 │
│   2. PG 自连接算 post↔post 共现数                                       │
│   3. sim(i,j) = cooccur / √(n_i · n_j)，每帖留 top-K                  │
│   4. 落 Redis ZSET  cf:item:{post_id}  (member=相似帖, score=sim)      │
└──────────────────────────────────────────────────────────────────────┘
                                      │
                                      │ 读路径（推荐 tab 请求时）
                                      ▼
┌──────────────────────────────────────────────────────────────────────┐
│ C5 协同过滤召回                                                        │
│   1. 读用户 seed：user:collect:posts:{uid} ∪ user:like:posts:{uid}     │
│   2. 批量 ZREVRANGE cf:item:{seed} 取各 seed 的相似帖                   │
│   3. 聚合 candidate → Σsim；剔除已点赞/收藏/浏览过的                     │
│   4. top X 候选 → SearchPostsByIDs hydrate → 喂给多路召回合并（C5 配额）  │
└──────────────────────────────────────────────────────────────────────┘
```

三个阶段解耦：**P0 灌数** / **P1 算相似** / **P2 接入召回**，各自可独立验证。

---

## 四、P0：`domains.post_interaction` 物化表

### 4.1 DDL（追加到 [docs/db.md](db.md)，由 DB owner 执行）

> 建表机制：本项目走 SQL 脚本管理（见 [pgsql/connect.go:55](../pkg/server/storage/db/pgsql/connect.go#L55)），运行时角色无 ALTER 权限，不用 AutoMigrate。与 post/circle/user 等表同路径。

```sql
-- 用户×帖子 交互矩阵（CF 隐反馈评分表）
CREATE TABLE domains.post_interaction (
    user_id   uuid        NOT NULL,
    post_id   uuid        NOT NULL,
    weight    smallint    NOT NULL,    -- 该 user-post 对的最强信号强度（1..5）
    ts        timestamptz NOT NULL,    -- 最近一次互动时间（CF 时间窗 + 清理用）
    PRIMARY KEY (user_id, post_id)
);

-- CF 共现自连接：按 post_id 取该帖所有互动者
CREATE INDEX idx_post_interaction_post    ON domains.post_interaction (post_id);
-- 候选筛选 / 清理
CREATE INDEX idx_post_interaction_post_ts ON domains.post_interaction (post_id, ts DESC);
CREATE INDEX idx_post_interaction_ts      ON domains.post_interaction (ts);
```

设计要点：
- **PK (user_id, post_id)**：一个 user-post 对只留一行，`weight` = 历史最强信号（max-ever），`ts` = 最近互动时间。
- **不带 action 列**（遵循需求 4 列）。代价：改权重映射后无法重算。若未来要支持重调权重，可加 `actions smallint` 位图列记录见过哪些动作；MVP 不加。
- **不存 deleted 标记**：取消点赞/收藏**不删行**（见 4.4 撤销语义）。

### 4.2 权重设计

| action | weight | 说明 |
|---|---|---|
| collect | 5 | 最强意图 |
| comment | 4 | 主动创作，强 |
| like | 3 | 显式正反馈 |
| comment_like | 2 | 旁路信号 |
| view | 1 | 隐式弱（量大） |

同 user-post 多种动作 → `weight = max(...)`。CF 共现计算可选用 weight 加权，也可只用二值（MVP 用二值，weight 留作 P3 加权相似度）。

### 4.3 灌数：事件驱动双写

**新增 Redpanda topic** `post_interaction` + 消息体（加到 [redpanda/constants.go](../pkg/server/storage/redpanda/constants.go)）：

```go
type InteractionAction string
const (
    InteractionView       InteractionAction = "view"
    InteractionLike       InteractionAction = "like"
    InteractionCollect    InteractionAction = "collect"
    InteractionComment    InteractionAction = "comment"
    InteractionCommentLike InteractionAction = "comment_like"
)

type PostInteractionMessage struct {
    UserID uuid.UUID        `json:"user_id"`
    PostID uuid.UUID        `json:"post_id"`
    Action InteractionAction `json:"action"`
    Weight int16            `json:"weight"`
    Ts     int64            `json:"ts"` // 事件时间 Unix 毫秒（消费者落 timestamptz）
}
```

**Producer**（加到 [redpanda/producer.go](../pkg/server/storage/redpanda/producer.go)，与 PublishPostHot 同构）：
```go
func PublishPostInteraction(userID, postID uuid.UUID, action InteractionAction, weight int16) error
// key = postID（保序 + 热点分片）；weight/action 由调用方按 4.2 映射传入
```

**5 个接线点**（各自 event publisher 多发一条，best-effort，失败仅日志——与 ApplyHotDelta/PublishPostHot 并列）：

| action | hook 位置 | 备注 |
|---|---|---|
| like | [like_event_publisher.PublishPostLike](../pkg/domains/like/infrastructure/like_event_publisher.go#L31) | 已有 userID/postID，加一行 |
| comment_like | [like_event_publisher.PublishCommentLike](../pkg/domains/like/infrastructure/like_event_publisher.go#L47) | postID 已冗余在消息，加一行 |
| collect | [collect_event_publisher.PublishPostCollect](../pkg/domains/collect/infrastructure/collect_event_publisher.go) | 加一行 |
| view | [history_event_publisher.PublishPostView](../pkg/domains/history/infrastructure/history_event_publisher.go#L21) | 已有 userID/postID，加一行 |
| comment | [comment_event_publisher](../pkg/domains/comment/infrastructure/comment_event_publisher.go) | ⚠️ 现 `PublishCommentHot(ctx, postID, dir)` **无 userID**。新增接口方法 `PublishCommentInteraction(ctx, userID, postID)`，由 [CreateComment](../pkg/domains/comment/application/service.go#L112)（有 userID）调用 |

> comment 这条因为现 publisher 签名缺 userID，需扩接口（加方法，不改原签名）。其余 4 处都是 publisher 里加一行 `redpanda.PublishPostInteraction(...)`。

**Consumer**（新增 [redpanda/interaction_consumer.go](../pkg/server/storage/redpanda/interaction_consumer.go)，结构照抄 [PostHotAggregator](../pkg/server/storage/redpanda/hot_consumer.go#L31) 的「ticker + 计数双触发」）：
- 聚合：无需跨消息累加（每条消息是完整交互），攒一批直接 upsert。
- flush 触发：每 ~2min 或 1000 条（交互量大于 hot，间隔短些）。
- 批量 upsert（复用 `jsonb_to_recordset` 模式）：

```sql
INSERT INTO domains.post_interaction (user_id, post_id, weight, ts)
SELECT v.user_id, v.post_id, v.weight, v.to_timestamp(v.ts_ms/1000.0)
FROM jsonb_to_recordset(?::jsonb) AS v(user_id uuid, post_id uuid, weight smallint, ts_ms bigint)
ON CONFLICT (user_id, post_id) DO UPDATE
SET weight = GREATEST(post_interaction.weight, EXCLUDED.weight),
    ts     = GREATEST(post_interaction.ts, EXCLUDED.ts);
```

### 4.4 幂等与撤销语义

- **幂等**：`ON CONFLICT ... GREATEST` → MQ 重投安全，不回退 weight/ts。
- **撤销（取消点赞/收藏）不删行**：这是隐反馈 CF 的标准做法（Koren 等的 implicit feedback）——一次瞬时点赞仍是兴趣信号。`weight = max-ever` 略乐观，对排序无害。
  - 若未来要支持「仅活跃交互」，加 `active boolean` 列由 undo 翻转；MVP 不做。
- **删帖/封禁**：不主动清理 interaction 行（CF 读路径 hydrate 时用 `SearchPostsByIDs` 过滤 `deleted=0 AND status=1`，失效帖自然不展示）。

### 4.5 清理

时间窗外的行定期清，控表体量。挂在 ItemCFSyncer（或独立 ticker）日跑：
```sql
DELETE FROM domains.post_interaction WHERE ts < now() - interval '120 days';
```
（保留略多于 CF 90 天窗，防边界抖动。）

---

## 五、P1：ItemCFSyncer（共现相似度计算）

新增 [redpanda/item_cf_syncer.go](../pkg/server/storage/redpanda/item_cf_syncer.go)，结构照抄 [CircleHotSyncer](../pkg/server/storage/redpanda/circle_hot_syncer.go#L28)（ticker + Stop + 批量）。**日级全量**（CF 容忍天级延迟），MVP 不做增量。

### 5.1 候选集界定（控量关键）

只对「值得计算 cf:item 的帖子」算，避免 N² 爆炸：
- **候选 C** = 近 30 天创建 + `deleted=0 AND status=1` 的帖子（可加 `hot > 阈值` 进一步收窄）。
- 仅对 `i, j ∈ C` 算共现。
- 理由：cf:item 用于「推荐」，相似帖必须是当前可推的；用户近期 seed 帖子也大概率落在 30 天内。

### 5.2 共现计算（PG 自连接）

```sql
-- 候选帖
WITH candidate AS (
    SELECT id FROM domains.post
    WHERE deleted = 0 AND status = 1
      AND create_time > now() - interval '30 days'
),
-- 每帖互动者数（相似度分母）
post_users AS (
    SELECT post_id, COUNT(*) AS n
    FROM domains.post_interaction
    WHERE ts > now() - interval '90 days'
      AND post_id IN (SELECT id FROM candidate)
    GROUP BY post_id
)
SELECT a.post_id AS i, b.post_id AS j, COUNT(*) AS cooccur
FROM domains.post_interaction a
JOIN domains.post_interaction b
  ON a.user_id = b.user_id AND a.post_id < b.post_id   -- 去对称、排除 i=j
WHERE a.ts > now() - interval '90 days'
  AND b.ts > now() - interval '90 days'
  AND a.post_id IN (SELECT id FROM candidate)
  AND b.post_id IN (SELECT id FROM candidate)
GROUP BY a.post_id, b.post_id
HAVING COUNT(*) >= 2;   -- min_cooccur，砍单次共现噪声
```

### 5.3 相似度 + top-K（Go 内存）

对查询结果，用 post_users 的 n_i/n_j 算余弦：
```
sim(i,j) = cooccur / sqrt(n_i * n_j)      ∈ (0, 1]
```
每帖保留 top-K（默认 50）相似帖，丢弃其余。

### 5.4 落 Redis

新增 key（[redis/constants.go](../pkg/server/storage/redis/constants.go)）：
```
cf:item:{post_id}   ZSET  member=相似 post_id, score=sim   TTL 48h
GetCFItemKey(postID) string
```

pipeline 批量写：对每个有相似结果的帖子 `DEL + ZADD + EXPIRE`（覆盖式刷新，幂等）。无相似结果的帖子不写 key（召回时 miss 跳过）。

### 5.5 热门帖爆炸 mitigation

候选限制已大幅缩量，但单帖互动者上万的头部帖自连接仍会产生巨量配对。缓解（按需开关）：
- **单帖互动者截断**：子查询对每帖取最近 N=2000 个互动者（`ORDER BY ts DESC LIMIT N`）再自连接。
- **min_cooccur 调高**：头部帖配对多但共现 ≥ 阈值的可控。
- **跳过头部超热帖**：`n > 上限` 的帖不参与（它们和谁都很像，区分度低）。

MVP 先不截断，上量后按实际 EXPLAIN 结果开。

---

## 六、P2：C5 协同过滤召回（接入推荐 tab）

> 前置依赖：推荐流本体（C1~C4 多路召回 + 交错合并）需先落地。本文只定义 C5 这一路的产出。

### 6.1 召回流程（每次推荐请求）

1. **取 seed**（用户强信号帖，pipeline 1 RTT）：
   - `ZREVRANGE user:collect:posts:{uid} 0 19`（收藏，top 20）
   - `ZREVRANGE user:like:posts:{uid} 0 29`（点赞，top 30）
   - 合并去重，≤50 个 seed。新用户无 seed → C5 空，自动降级。
2. **查相似**（pipeline 1 RTT）：对每个 seed `ZREVRANGE cf:item:{seed} 0 N-1 WITHSCORES`（N=20）。
3. **聚合**（Go 内存）：`candidate_post → Σ sim`（被多个 seed 指向的候选得分累加）。这是 CF 预测分。
4. **剔除已交互**：candidate 命中用户的 like/collect/view ZSET 的丢弃（用 ZSCORE 批量判存在，或集合差集）。
5. **取 top X**（默认 40）候选 post_id → 调 [SearchPostsByIDs](../pkg/server/storage/elasticsearch/post.go#L179) hydrate（自动过滤失效帖）。
6. 作为 **C5** 喂给多路召回合并，按配额参与交错排序。

### 6.2 配额

C5 从原 C2（全局热门）配额里切，例如调整为 C1 35% / C2 25% / **C5 15%** / C3 15% / C4 10%（具体数值待推荐流联调）。CF 强在「个性化长尾」，不强占大头。

### 6.3 冷启动

- **新用户**（无点赞/收藏）→ 无 seed → C5 空 → C1/C2 兜底。这就是 CF 必须是「加法」的原因。
- **新帖**（无交互）→ 进不了相似网 → 靠 C4（最新）曝光攒数据。可对新帖给探索流量加成（P3）。

---

## 七、配置项（新增 conf.Recommend.CF）

加到 [conf.go](../pkg/conf/conf.go)，`configs/config.yaml` 同步：

```yaml
recommend:
  cf:
    enabled: true
    interaction_window_days: 90      # 共现计算回溯窗口
    candidate_fresh_days: 30         # 候选帖创建时间窗
    min_cooccur: 2                   # 最小共现次数（砍噪声）
    topk: 50                         # 每帖保留相似帖数
    seed_collect: 20                 # 召回取收藏 seed 数
    seed_like: 30                    # 召回取点赞 seed 数
    recall_top: 40                   # C5 输出候选数
    zset_ttl_hours: 48               # cf:item ZSET TTL
    sync_cron: "0 3 * * *"           # ItemCFSyncer 每日 03:00（或用 ticker 24h）
```

Redpanda topic 配置（加到 `conf.Redpanda`）：
```yaml
redpanda:
  post_interaction_topic: "post_interaction"
  post_interaction_consumer_group: "post_interaction_cg"
  post_interaction_flush_interval: 2   # min
  post_interaction_flush_messages: 1000
```

---

## 八、一致性 / 边界 / 风险

| 项 | 处理 |
|---|---|
| MQ 重投 | `ON CONFLICT GREATEST` 幂等 |
| 消费者宕机 | interaction 落库延迟，CF 相似度天级延迟可接受 |
| 相似度过期 | cf:item ZSET TTL 48h + 每夜全量刷新 |
| 失效帖（删/封） | 读路径 SearchPostsByIDs 过滤，不展示 |
| 头部帖爆炸 | 候选限制 + min_cooccur + 可选互动者截断（5.5） |
| 新用户冷启动 | C5 空，降级到 C1/C2 |
| 隐私 | 交互数据仅用于本人物化推荐，不跨用户暴露 |
| 表体量 | 90 天窗 + 日清理 DELETE，有界 |

---

## 九、分阶段交付

| 阶段 | 内容 | 可验证标志 | 依赖 |
|---|---|---|---|
| **P0** | post_interaction 表 + 5 action 双写 + topic + InteractionConsumer | 表里有数据，权重正确 | 无 |
| **P1** | ItemCFSyncer 日跑共现 → cf:item ZSET | 抽样查 `cf:item:{pid}` 有合理相似帖 | P0 |
| **P2** | C5 召回接入推荐 tab | 推荐 tab 出现 CF 来源帖 | P1 + 推荐流本体 |
| **P3**（可选） | 增量相似度 / 时间衰减相似度 / 加权共现 / 新帖探索流量 / 权重重调 | — | P2 |

P0、P1 可在推荐流本体之前独立推进；P2 等推荐流落地。

---

## 十、待决策点

1. **comment 接入方式**：扩 `CommentEventPublisher` 加 `PublishCommentInteraction`（推荐），还是给 `PublishCommentHot` 加 userID 参数？倾向前者（不改原签名）。
2. **view 是否进 P0**：量大、噪声高，但给冷帖覆盖。默认进 P0（weight=1），依赖现有浏览去重+ZSET cap 500 控量。要否延到 P1？
3. **候选帖是否加 hot 阈值**：进一步收窄候选集，但可能漏掉新冷帖的相似关系。默认不加，仅按时间窗。
4. **配额**：C5 切多少（默认 15%）？等推荐流联调定。
5. **Syncer 调度**：用 cron 表达式（精确 03:00）还是 24h ticker（简单）？倾向 ticker，与 CircleHotSyncer 一致。

---

## 十一、已实现范围

**P0（post_interaction 灌数）✅**
- DDL：[docs/db.md](db.md) 追加 `domains.post_interaction(user_id, post_id, weight, ts)` + 3 索引
- 配置：[conf.go](../pkg/conf/conf.go) 加 `Recommend.CF` 结构体 + Redpanda `PostInteraction*` 4 字段；[config.yaml](../configs/config.yaml) 同步
- 消息/Producer：[constants.go](../pkg/server/storage/redpanda/constants.go) `PostInteractionMessage` + `InteractionAction`/`InteractionWeight*`；[producer.go](../pkg/server/storage/redpanda/producer.go) `PublishPostInteraction`
- Consumer：[interaction_consumer.go](../pkg/server/storage/redpanda/interaction_consumer.go)（镜像 PostHotAggregator，2min/1000 条双触发，`ON CONFLICT GREATEST` 幂等 upsert）
- 5 个 publisher 接线（仅正向写矩阵，撤销不删行）：
  - like：[PublishPostLike](../pkg/domains/like/infrastructure/like_event_publisher.go)（like/3）、[PublishCommentLike](../pkg/domains/like/infrastructure/like_event_publisher.go)（comment_like/2）
  - collect：[PublishPostCollect](../pkg/domains/collect/infrastructure/collect_event_publisher.go)（collect/5）
  - view：[PublishPostView](../pkg/domains/history/infrastructure/history_event_publisher.go)（view/1）
  - comment：扩 [CommentEventPublisher](../pkg/domains/comment/domain/repository.go) 接口加 `PublishCommentInteraction`（comment/4），[CreateComment](../pkg/domains/comment/application/service.go) 调用
- 装配：[server.go](../cmd/apps/server.go) 8.17 启 producer+consumer；关停 `ClosePostInteractionProducer`

**P1（ItemCFSyncer 相似度）✅**
- Redis key：[constants.go](../pkg/server/storage/redis/constants.go) `cf:item:{post_id}` ZSET + `GetCFItemKey`
- Syncer：[item_cf_syncer.go](../pkg/server/storage/redpanda/item_cf_syncer.go)（镜像 CircleHotSyncer，24h ticker）
  - 共现 SQL（candidate CTE + 自连接 i<j + `HAVING min_cooccur`）→ cosine `sim=cooccur/√(n_i·n_j)` → 对称累积 → top-K（默认 50）分批 pipeline 写 ZSET（DEL+ZADD+EXPIRE，TTL 48h）
  - 附带清理：每轮 `DELETE WHERE ts < cleanup_days`
- 装配：[server.go](../cmd/apps/server.go) 8.18（仅 `recommend.cf.enabled=true` 启动）；关停 `StopItemCFSyncer`

**P2（推荐流本体 + C5 召回接入）✅**

新建 `recommend` 域（跨域编排器，无聚合根），端点 `GET /post/home?tab=recommend`：

- **新域 8 文件**：
  - [domain/ports.go](../pkg/domains/recommend/domain/ports.go) — 端口 HomeFeedSearcher/PostHydrator/PostMetaReader/CircleLookup/SeedReader/InteractionChecker/FeedCache + DTO（FeedPostItem 含 IsLiked/IsCollected、FeedPage）
  - [application/service.go](../pkg/domains/recommend/application/service.go) — GetHomeFeed：池 miss/token 过期→重建 → LRANGE → hydrate → 补交互态 → 返回（含 pool_token）
  - [application/recall.go](../pkg/domains/recommend/application/recall.go) — 5 路（C1 兴趣圈子/C2 全局热门/C3 行为圈子/C4 最新/C5 CF）+ 交错/dedup/剔除已交互/C2 兜底；每路 try/log
  - [infrastructure/home_feed_searcher_es.go](../pkg/domains/recommend/infrastructure/home_feed_searcher_es.go) — wrap SearchHomeFeed，返回纯 ID
  - [infrastructure/feed_cache_redis.go](../pkg/domains/recommend/infrastructure/feed_cache_redis.go) — wrap feed:recommend:{uid} LIST + token
  - [infrastructure/seed_reader_redis.go](../pkg/domains/recommend/infrastructure/seed_reader_redis.go) — like/collect/view ZSET 读 + cf:item pipeline 聚合
  - [infrastructure/interaction_checker_redis.go](../pkg/domains/recommend/infrastructure/interaction_checker_redis.go) — BatchCheck is_liked/is_collected
  - [interfaces/http/{handler,routes}.go](../pkg/domains/recommend/interfaces/http/routes.go) — GET /post/home
- **基建（P0 之外新增）**：
  - [elasticsearch/post.go](../pkg/server/storage/elasticsearch/post.go) — `SearchHomeFeed(sort, circleIDs, size, searchAfter)` 泛化 SearchCirclePosts（circleIDs 可选 + hot/latest）
  - [redis/history_lua.go](../pkg/server/storage/redis/history_lua.go) — `ListPostLikedIDs/ListPostCollectedIDs`（镜像 ListPostViews）
  - [redis/feed_pool.go](../pkg/server/storage/redis/feed_pool.go) — 候选池 LIST + 版本 token（Build/Len/Range/Token/Exists）
  - [redis/constants.go](../pkg/server/storage/redis/constants.go) — `feed:recommend:` + token key
  - [conf.go](../pkg/conf/conf.go) — `Recommend.Feed`（pool_size/ttl/quotas/exclude_interacted）
- **跨域方法**：[circle.ListJoinedCircleIDs](../pkg/domains/circle/application/service.go)（joinedCache + 重建）、[post.ListCircleIDsByPostIDs](../pkg/domains/post/application/service.go)（C3 反查）
- **装配**：[composition](../pkg/composition/facade_bridges.go) 3 桥接（recommendPostHydrator/recommendCircleLookup/recommendPostMetaReader）+ newRecommendService + registerRecommend

**关键决策落地**：候选池 + pool_token 防翻页错位（token 不匹配回 offset=0 + pool_refreshed）；C3 走 DB ListByIDs 反查 circle_id；is_liked/is_collected 由 recommend 域 InteractionChecker 自补（不改 post.PostListItem 签名）；channel 独立降级 + C2 兜底，feed 永不空；匿名 401。

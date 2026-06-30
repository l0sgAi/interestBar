# 近期活跃圈子分页列表 设计方案

> 目标：新增 `GET /circle/active` —— 按近期活跃度排序的圈子分页列表。
>
> 统计维度（已确认）：**近 7 天该圈子新增的已发布帖子数**（recent post count）。
> 查询策略（已确认）：**ES 聚合**（在 posts 索引上做 terms 聚合 + 时间窗口过滤）。

---

## 一、为什么不是 `ORDER BY hot`

现有 `domains.circle.hot` 是**累积终身热度**（[hot-sync-design.md O4](hot-sync-design.md)）：

- 写入：`UPDATE hot = GREATEST(hot + Δ, 0)`，只增不减（除 undo），**永不衰减**。
- 来源：post hot Δ → `circle:hot:{cid}` Redis 累加器 → `CircleHotSyncer` 34min SCAN+GETDEL → 批量 UPDATE。`GETDEL` 即清零，**无滚动窗口历史**。
- 后果：2 年前爆款圈子 hot=100000 长期霸榜，"近期活跃"名不副实。P5 时间衰减 `hot_decay` **未实现**。

故 `hot` 列**不能**直接当"近期活跃"。需要一个**带时间窗口**的活跃信号，最直接、语义最干净的 = **窗口内发帖数**。

---

## 二、数据源盘点

| 信号 | 来源 | 是否带时间窗口 | 选用 |
|---|---|---|---|
| 近 7d 发帖数 | `domains.post`（`circle_id` + `create_time` + `deleted` + `status`），已 CDC 进 ES | ✅ | ✅ 本方案 |
| `circle.hot` 累积 | `domains.circle.hot` | ❌ 终身累积 | ❌（仅作展示字段） |
| `circle.post_count` 累积 | `domains.circle.post_count` | ❌ 终身累积 | ❌（仅作展示字段） |
| `circle:hot:{cid}` Δ | Redis 累加器 | ❌ 34min 清零无历史 | ❌ |

ES posts 索引已有字段（[elasticsearch/post.go:13](../pkg/server/storage/elasticsearch/post.go#L13)）：`circle_id` / `create_time` / `deleted` / `status` / `hot`，且已被 `SearchPosts` 用 `term` 查 `circle_id` 佐证可按字段过滤。

---

## 三、查询方案对比（选型理由）

| 方案 | 读延迟 | 新增基建 | 规模风险 | 结论 |
|---|---|---|---|---|
| **A. ES terms 聚合（本方案）** | 单次聚合 RTT | 无（复用 CDC posts 索引） | 窗口内文档数受控；ES request cache 命中 | ✅ |
| B. Redis ZSET + 定时 job | O(logN) 极快 | 新增 job（仿 `CircleHotSyncer`）+ 全局 ZSET | ZSET 需全量重算；N 分钟延迟 | ❌ 过重 |
| C. DB `GROUP BY` + 游标 | 每次全表扫描 | 无 | posts 表大时慢 | ❌ 规模差 |

**为何不选 ES `composite` 聚合做深分页**：`composite` 按 source key（circle_id）升序翻页，**无法按活跃度（doc_count）排序**。按指标排序的列表本质上只能用 `terms` 聚合 + `order: {_count: desc}`，其分页用 `bucket_sort` 切片（见下）。这是 ES 对"趋势榜分页"的标准做法，权衡见 [§六 边界](#六边界--一致性--风险)。

---

## 四、ES 查询设计

### 4.1 聚合体（核心）

```jsonc
GET pg.public.post/_search
{
  "size": 0,
  "query": {
    "bool": {
      "filter": [
        { "term":  { "deleted": 0 } },
        { "term":  { "status": 1 } },
        { "range": { "create_time": { "gte": "now-7d/d" } } }
      ]
    }
  },
  "aggs": {
    "by_circle": {
      "terms": {
        "field": "circle_id.keyword",
        "size": 500,
        "order": { "_count": "desc" }
      },
      "aggs": {
        "page": { "bucket_sort": { "from": 0, "size": 20 } }
      }
    },
    "active_total": {
      "cardinality": { "field": "circle_id.keyword", "precision_threshold": 1000 }
    }
  }
}
```

要点：
- `size: 0` —— 不要 hits，只要聚合桶，省带宽/解析。
- `filter`（非 must）—— 纯结构化过滤，跳过相关性打分，命中 ES request cache。
- `terms.size = 500`（**maxScan**）—— 只排前 500 个活跃圈子，足够覆盖任何"近期活跃榜"UI；超出截断，响应带 `truncated` 标志（复用 [MyCircleSearchResult.Truncated](../pkg/domains/circle/application/service.go#L106) 语义）。
- `bucket_sort` —— 在按 `_count desc` 排好的桶上切片 `[from, from+size)`，**ES 原生分页**，应用层无需自己切片。
- `active_total` —— `cardinality` 给近似活跃圈子总数（供前端显示"共 N 个活跃圈子"）。

### 4.2 ⚠️ 前置确认（实现第一步）

`circle_id` 必须是 **`keyword`** 类型才能 terms 聚合。CDC 动态映射下：
- 若已是 `keyword` → 用 `circle_id`。
- 若是 `text`（带 `.keyword` 子字段）→ 用 `circle_id.keyword`。

`create_time` 需为 `date`（range 过滤）；若被 CDC 映射成 `text`，range 失效，需在索引模板里显式声明 `date`。**实现前先 `GET pg.public.post/_mapping` 确认这两个字段类型**，必要时加 ES index template（一次性运维动作，不改 Go）。

### 4.3 分页策略

- 入参：`size`（每页，默认 20，上限 100，复用 [normalizeSize](../pkg/domains/circle/interfaces/http/handler.go#L296)）+ `offset`（0 基，默认 0）。
- 游标：`offset`（非 `search_after`）。理由：按指标排序的榜无法用 composite `after_key`；offset 简单且语义对。
- 每次请求重跑聚合（参数相同时 ES request cache 秒级命中，几乎免费）。
- 终止：返回桶数 < `size`，或 `offset + size >= maxScan(500)`。
- **翻页期间数据变动导致排名漂移**：可接受（趋势榜本性如此），文档注明。

### 4.4 明细组装

聚合只给 `circle_id` + `recent_post_count`（桶 `doc_count`）。圈子展示信息（name/avatar/member_count/post_count/hot）走 **DB `repo.GetByIDs`**（[circle_repo_pg.go:43](../pkg/domains/circle/infrastructure/circle_repo_pg.go#L43) 已存在，权威、含全部字段、无需新写 ES by-ids 查询）。

> 备选：从 circle ES 索引按 ids 取（与 `SearchCircles` 路径一致）。本方案选 DB 以减少对 circle ES CDC 一致性的依赖，且 `GetByIDs` 现成。

过滤掉已删除/非正常状态圈子（`GetByIDs` 已 `deleted=0`；`status != Normal` 的在应用层跳过，保持桶顺序不断层）。

---

## 五、API 设计

### 路由
```
GET /circle/active   需登录（挂 authCheck 组，与 /circle/list 一致）
```
注册位置：[circle/interfaces/http/routes.go](../pkg/domains/circle/interfaces/http/routes.go)（在 `/circle/list` 旁加一行）。

### 请求（query）
| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `size` | int | 20 | 每页数，上限 100 |
| `offset` | int | 0 | 0 基偏移 |

### 响应 VO
```go
// ActiveCircleDoc 近期活跃圈子项。
type ActiveCircleDoc struct {
    ID              string `json:"id"`
    Name            string `json:"name"`
    AvatarURL       string `json:"avatar_url,omitempty"`
    Description     string `json:"description,omitempty"`
    CategoryID      string `json:"category_id,omitempty"`
    MemberCount     int    `json:"member_count"`
    PostCount       int    `json:"post_count"`        // 累积（circle.post_count）
    Hot             int    `json:"hot"`               // 累积（circle.hot）
    RecentPostCount int    `json:"recent_post_count"` // ★ 近 7d 发帖数（活跃信号）
    JoinType        int16  `json:"join_type"`
    CreateTime      string `json:"create_time"`
}

type ActiveCircleResult struct {
    Circles []ActiveCircleDoc `json:"circles"`
    Total   int64             `json:"total"`     // 活跃圈子近似总数（cardinality）
    Size    int               `json:"size"`
    Offset  int               `json:"offset"`
    Truncated bool            `json:"truncated,omitempty"` // 触达 maxScan 上限
}
```

---

## 六、边界 / 一致性 / 风险

| 点 | 处理 |
|---|---|
| `circle_id` 非 keyword | 上线前确认 mapping；必要时加 index template（运维，非 Go 改动） |
| `create_time` 非 date | 同上 |
| 深分页排名漂移 | 趋势榜本性；文档注明，UI 建议只翻前几页 |
| 聚合翻页上限 | `maxScan=500` 截断，`truncated=true` 提示 |
| 7d 窗口扫文档量 | ES filter cache + request cache 友好；窗口内已发布帖量可控。若后续圈子/帖量剧增，可调小窗口或加定时预算 |
| ES 不可用 | 聚合接口直接降级返回 5xx（与现有 SearchCircles 行为一致，不做 DB 兜底，避免慢查询） |
| 冷启动无数据 | 返回空列表 + total=0，正常 |
| 活跃榜含被禁圈子 | `GetByIDs` 跳过 `deleted`；`status != Normal` 应用层过滤 |

---

## 七、DDD 分层改动清单

| 层 | 文件 | 改动 |
|---|---|---|
| **elasticsearch** | [elasticsearch/post.go](../pkg/server/storage/elasticsearch/post.go)（或新 `post_agg.go`） | 新增 `AggregateActiveCircles(windowDays, size, offset int) (*ActiveCircleAggResult, error)`：发 §4.1 聚合体，解析桶 → `[{circleID, recentCount}]` + total |
| **infrastructure** | [circle/infrastructure/circle_searcher_es.go](../pkg/domains/circle/infrastructure/circle_searcher_es.go) | `CircleSearcher` 接口加 `SearchActive(ctx, size, offset) (*RawActiveCircleResult, error)`；impl 调上面的聚合 |
| **application** | [circle/application/service.go](../pkg/domains/circle/application/service.go) | `CircleService` 加 `ListActiveCircles(ctx, size, offset int) (*ActiveCircleResult, error)`：聚合拿 ranked ids+recent_count → `repo.GetByIDs` 组装 `ActiveCircleDoc`（保留聚合顺序） |
| **interfaces/http** | [circle/interfaces/http/handler.go](../pkg/domains/circle/interfaces/http/handler.go) | 加 `GetActiveCircles` handler + `GetActiveCirclesRequest{Size, Offset}` |
| **interfaces/http** | [circle/interfaces/http/routes.go](../pkg/domains/circle/interfaces/http/routes.go) | `cir.GET("/active", h.GetActiveCircles)` + 路由清单注释 |
| **composition** | （如有显式 wiring 才改） | service 已注入 searcher/repo，新方法无需新依赖，通常**无需改 composition** |

> 无 DB schema 变更、无新 Redis key、无新 MQ topic、无新定时任务。

---

## 八、分阶段交付

| 阶段 | 内容 | 验收 |
|---|---|---|
| **S0 确认 mapping** | `GET pg.public.post/_mapping` 看 `circle_id`/`create_time` 类型；不合规加 index template | 字段类型 = keyword / date |
| **S1 ES 聚合** | `AggregateActiveCircles` + 解析 + 单测（mock ES 响应） | 桶顺序按 doc_count desc、分页切片正确 |
| **S2 领域贯通** | searcher 方法 + service `ListActiveCircles` + GetByIDs 组装 | ranked ids 顺序保留、recent_post_count 正确回填 |
| **S3 HTTP** | handler + route | `GET /circle/active?size=20&offset=0` 返回 VO；offset 翻页生效；超 maxScan 返回 truncated |
| **S4 联调** | 真 ES 验证 7d 窗口、ranking、明细字段 | 端到端 |

---

## 九、未决 / 可后续

- 是否同时返回"近 7d 活跃成员数"（`cardinality` user_id 子聚合）—— 当前维度只要发帖数，预留子聚合位。
- 窗口/上限/maxScan 是否进 `conf`（参考 `conf.Hot` 模式）—— 先写常量，需热调时再提配置。
- 若产品要"近期热度增量"维度，需先建滚动窗口（circle:hot 现 34min 清零无历史），属另一独立工作。

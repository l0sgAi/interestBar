# 热点榜单兜底优化 设计方案

> 目标：当某个时间窗（24h/7d）内无新帖导致榜单为空时，自动用「全局热门」兜底填充，
> 避免热点页（强曝光入口）对用户呈现空列表。
>
> 范式与 [trending-design.md](trending-design.md) / [active-circles-design.md](active-circles-design.md) 一致。
> 本文是 trending 领域的**增量优化**，不推翻原设计，仅补足「空窗口」场景。

---

## 一、问题背景

### 1.1 现状缺陷

[trending-design.md §五](trending-design.md) 的 `TrendingRankSyncer` 对每个榜单（3 维度 × 2 窗口 = 6 个）
跑「窗口化 ES 聚合」。当窗口内无满足条件（`deleted=0 + status=1 + create_time ∈ 窗口`）的帖子时，
聚合返回 0 条，`syncOne` 写入空 ZSET，导致该榜单**为空**。

热点页是强曝光入口（首屏 Tab / 独立页面），空列表体验差，用户误以为「社区没内容」或「接口坏了」。

### 1.2 实测证据

测试库（`pg.domains.post`）：

| 查询 | 结果 |
|---|---|
| 最新帖 `create_time` | `2026-06-27T13:24:53Z`（标题"刷热度"，hot=22） |
| 近 24h 满足条件帖子数（`deleted=0 + status=1 + create_time≥now-24h/h`） | **0** |
| 近 7d 满足条件帖子数 | 2 |

→ 默认 `window=24h` 时，ES 窗口聚合返回 0 条，接口返回：

```jsonc
{ "code": 200, "data": { "window": "24h", "refreshed_at": 0, "size": 10 } }
// posts/circles/users 均空
```

7d 榜单正常（Redis 有 `trending:post/circle/user:7d` 且有数据），24h 榜单空。
根因是**数据稀疏 + 严格时间窗**叠加，非代码 bug。

### 1.3 兜底的必要性

时间窗是 trending 的核心语义（"近期最热"），但社区早期/冷启动/低活跃时段，窗口内可能无数据。
需要一个兜底：**窗口空时退化为全局热门**，保证榜单「总有内容」。

---

## 二、设计决策与取舍

### 2.1 兜底放置层（三方案对比）

| 方案 | 读路径改动 | 新增 ZSET | 延迟 | 前端可标识兜底 | 选用 |
|---|---|---|---|---|---|
| **A. job 层（写时兜底）** | ❌ 无 | ❌ 无 | 固定（同步器周期） | ❌ 不可 | ✅ **本方案** |
| B. 读路径层（查时兜底） | ✅ 需改 service | ❌ 无 | 每次 ES RTT | ✅ 可加 fallback 标志 | ❌ 高并发 ES 压力 |
| C. 双层（job 预算全局榜 + 读路径回退） | ✅ 需改 service | ✅ +3 ZSET | 固定 | ✅ 可加标志 | ❌ 过重 |

**选 A（job 层）的理由**：
- 读路径零改动（`TrendingService.GetTrending` 与 DTO 完全不变），降低回归风险。
- 不新增 ZSET/key（兜底数据写进**同一个** `trending:{dim}:{window}` ZSET），保持 key 模型简洁。
- 延迟固定且与榜单刷新周期一致（默认 5min），可预期。
- **代价**：响应无法告知前端「这是兜底数据」——这是明确接受的取舍（见 §九）。若后续产品强需求，再演进到方案 C。

### 2.2 兜底数据信号（三方案对比）

| 信号 | ES 实现 | 结果质量 | 选用 |
|---|---|---|---|
| **全局热门（无窗口 hot）** | 去掉 `range create_time`，其余不变 | 复用现有 hot 子系统；可能老爆款霸榜 | ✅ **本方案** |
| 扩窗兜底（24h 空 → 30d） | 窗口参数化、扩大 range | 比"全局"更近，但仍可能含旧内容 | ❌ 窗口语义混乱 |
| latest（最新内容） | 换排序键 `create_time desc` / doc_count | 语义多样，圈子/用户榜逻辑要另写 | ❌ 实现复杂 |

**选「全局热门」的理由**：
- 三类榜单**信号统一**：帖子=hot desc，圈子/用户=无窗口 terms+sum(hot)。复用现有 `AggregateTrending`，只加一个「无窗口」开关。
- 与 hot 子系统同源（[hot-sync-design.md](hot-sync-design.md)），语义自洽。
- 老爆款霸榜风险仅在「窗口完全空」时触发（此时本就没近期内容可展示），属可接受的退化。

---

## 三、兜底触发条件

### 3.1 严格「仅空才兜底」

**仅当窗口聚合返回 0 条**（`len(agg.Items) == 0`）才触发兜底。

- ✅ 窗口为空（0 条）→ 兜底
- ❌ 窗口稀疏（如 2 条）→ **不兜底**，维持窗口真实数据

### 3.2 为何不做「稀疏兜底」（< N 条补足）

考虑过「窗口不足 N 条时补全局热门凑够」的方案，但**否决**，原因：

| 问题 | 说明 |
|---|---|
| 语义混合 | 同一 ZSET 混入「窗口热」+「全局热」两种数据，读路径无法区分，排序失真（窗口内 2 条 + 全局补 18 条，全局的可能比窗口的还靠前） |
| 去重复杂度 | 窗口数据与兜底数据可能重叠（同一帖既在窗口又在全局榜），需合并去重逻辑 |
| 阈值难定 | N 取多少都主观，不同维度/窗口合适值不同 |

严格「仅空才兜底」下，ZSET 要么纯窗口数据、要么纯兜底数据，**不混合**，语义干净、无去重问题。

---

## 四、兜底数据源：无窗口 ES 聚合

兜底查询 = 窗口查询**去掉 `range create_time` filter**，其余（维度分发、排序、聚合体）完全一致。

### 4.1 帖子榜兜底（无窗口 hits + hot desc）

```jsonc
GET pg.domains.post/_search
{
  "size": 100,
  "query": {
    "bool": {
      "filter": [
        { "term": { "deleted": 0 } },
        { "term": { "status": 1 } }
        // ★ 无 range create_time —— 兜底不限制时间窗
      ]
    }
  },
  "sort": [
    { "hot": { "order": "desc" } },
    { "id":  { "order": "desc" } }
  ],
  "_source": ["id", "hot"]
}
```

与 [trending-design.md §3.2](trending-design.md) 帖子榜的唯一差异：**去掉 `range create_time`**。

### 4.2 圈子/用户榜兜底（无窗口 terms + sum(hot)）

```jsonc
GET pg.domains.post/_search
{
  "size": 0,
  "query": {
    "bool": {
      "filter": [
        { "term": { "deleted": 0 } },
        { "term": { "status": 1 } }
        // ★ 无 range create_time
      ]
    }
  },
  "aggs": {
    "by_circle": {                          // 用户榜换 "by_user" + "user_id"
      "terms": {
        "field": "circle_id",
        "size": 500,
        "order": { "hot_sum": "desc" }
      },
      "aggs": {
        "hot_sum": { "sum": { "field": "hot" } }
      }
    }
  }
}
```

兜底用「终身 Σhot」（无窗口），可能与「窗口 Σhot」差异较大，但仅在窗口空时发生，可接受。

---

## 五、三类榜单的兜底信号

| 榜单 | 窗口版信号 | 兜底版信号（无窗口） | 差异 |
|---|---|---|---|
| 热门帖子 | 窗口内 post.hot | 全局 post.hot | 去掉时间窗，取终身最热帖 |
| 热门圈子 | 窗口内 Σhot（该圈子窗口内发帖热度之和） | 终身 Σhot（该圈子所有发帖热度之和） | 与 `domains.circle.hot` 累积列同源语义 |
| 热门用户 | 窗口内 Σhot | 终身 Σhot | 同上 |

> 注意：兜底版圈子/用户的「终身 Σhot」本质等价于 `circle.hot` / 潜在的 `user.hot` 累积列，
> 但**不读 DB 累积列**——仍走 ES 聚合（`terms + sum(hot)`），与窗口版同一条代码路径，
> 仅 filter 不同。保持实现统一，避免为兜底单独写 DB 查询。

---

## 六、job 层兜底流程

### 6.1 syncOne 改动（伪码）

现状（[trending_syncer.go:95](../pkg/server/storage/redpanda/trending_syncer.go#L95)）：
```
syncOne(dim, window):
    agg = AggregateTrending(dim, window, topN)
    items = toScoredIDs(agg.Items)
    rewriteTrendingBoard(dim, window, items)   // 即使空也写（更新 refreshed_at）
```

改动后：
```
syncOne(dim, window):
    agg = AggregateTrending(dim, window, topN)         // ① 窗口聚合
    if err != nil: return err                          //    ES 错误 → 本轮跳过（保留上轮，见 §八）
    items = toScoredIDs(agg.Items)

    if len(items) == 0:                                // ② 窗口空 → 兜底
        fbAgg = AggregateTrending(dim, "", topN)       //    无窗口聚合（全局热门）
        if fbErr == nil:
            items = toScoredIDs(fbAgg.Items)           //    兜底也空就保持 items=[]（确实全局都没数据）

    rewriteTrendingBoard(dim, window, items)           // ③ 写同一个 ZSET + 更新 refreshed_at
```

### 6.2 流程图

```
                  ┌─────────────────────────┐
                  │ syncOne(dim, window)    │
                  └────────────┬────────────┘
                               ▼
                  ┌─────────────────────────┐
                  │ ① 窗口聚合               │
                  │ AggregateTrending       │
                  │   (dim, window, topN)   │
                  └────────────┬────────────┘
                               │
                  ┌────────────▼────────────┐
                  │ ES 错误？                │──是──▶ return err（保留上轮，best-effort）
                  └────────────┬────────────┘
                            否 │
                  ┌────────────▼────────────┐
                  │ ② 窗口结果为空？          │
                  └────────────┬────────────┘
                  是 │                   │ 否
                     ▼                   │
        ┌────────────────────────┐       │
        │ 兜底：无窗口聚合         │       │
        │ AggregateTrending       │       │
        │   (dim, "", topN)       │       │
        └────────────┬───────────┘       │
                     │ items = 兜底结果    │
                     └────────┬──────────┘
                              ▼
                  ┌─────────────────────────┐
                  │ ③ 写同一个 ZSET          │
                  │ trending:{dim}:{window} │
                  │ + 更新 refreshed_at     │
                  └─────────────────────────┘
```

---

## 七、实现影响点（最小改动）

| 层 | 文件 | 改动 |
|---|---|---|
| **elasticsearch** | [trending_agg.go](../pkg/server/storage/elasticsearch/trending_agg.go) | `AggregateTrending` 支持无窗口：`window=""` 时不附加 `range create_time` filter（`:53-62` 的 filter 构造加判断）；`trendingWindowGTE`（`:105`）对空串返回"无窗口"语义 |
| **redpanda** | [trending_syncer.go](../pkg/server/storage/redpanda/trending_syncer.go) | `syncOne`（`:95`）增加兜底分支：窗口聚合空 → 调一次 `AggregateTrending(dim, "", topN)` → 用结果填充 |
| ~~读路径~~ | ~~service.go / handler.go~~ | **不改**（兜底对前端透明） |
| ~~DTO~~ | ~~domain/ports.go~~ | **不改**（无新字段） |
| ~~Redis key~~ | ~~constants.go~~ | **不改**（兜底写同一个 ZSET） |
| ~~配置~~ | ~~conf.go / config.yaml~~ | **不改**（兜底用现有 topN） |

### 7.1 `AggregateTrending` 无窗口支持（预备改动）

`window=""` 表示「无窗口（兜底）」。filter 构造改为：

```go
filter := []map[string]interface{}{
    {"term": map[string]interface{}{"deleted": 0}},
    {"term": map[string]interface{}{"status": 1}},
}
if window != "" {                                    // ★ 仅当指定窗口才附加 range
    windowGTE, ok := trendingWindowGTE(window)
    if !ok {
        return nil, fmt.Errorf("unsupported trending window: %q", window)
    }
    filter = append(filter, map[string]interface{}{
        "range": map[string]interface{}{"create_time": map[string]interface{}{"gte": windowGTE}},
    })
}
```

其余（维度分发、查询体、解析）完全不变。`window=""` 时合法取值只有聚合层内部与 syncer 用，
不暴露给读路径（`normalizeWindow` 仍只认 `24h`/`7d`）。

---

## 八、降级与一致性

| 场景 | 处理 |
|---|---|
| 窗口聚合 ES 错误 | `syncOne` 返回 err，**本轮跳过**（保留上轮 ZSET + 上轮 refreshed_at）；与现有降级一致（[trending-design.md §五](trending-design.md)） |
| 窗口聚合成功但空 → 兜底聚合 ES 错误 | 兜底失败也跳过（保留上轮）；记日志 |
| 窗口空 + 兜底也空（全局都没满足条件的数据） | `items=[]`，写空 ZSET + 更新 refreshed_at（refreshed_at 有值但数组空 = "确实没数据"） |
| 窗口有数据 | 不兜底，维持现状（ZSET 纯窗口数据） |

**关键不变式**：兜底只在「窗口完全空」时触发，ZSET 永远是「纯窗口数据」或「纯兜底数据」之一，不混合（见 §3.2）。

---

## 九、前端可观测性

### 9.1 响应不带 fallback 标志

选「job 层兜底」的固有取舍：兜底数据写进同一个 ZSET，读路径（`TrendingService`）无从得知数据来源，
**响应里没有 `is_fallback` 字段**。

### 9.2 前端如何间接判断

| 信号 | 含义 |
|---|---|
| `refreshed_at` 有值 + 数组非空 | 正常榜单（可能是窗口数据，也可能是兜底数据——前端无需区分，展示一致） |
| `refreshed_at` 有值 + 数组空 | job 跑过了，但窗口和全局都没满足条件的数据（极少见，冷启动） |
| `refreshed_at: 0` | job 从未成功跑过 / ES 持续故障降级中 |

### 9.3 后续演进（非本次范围）

若产品强需求「前端区分兜底」，演进路径：
1. job 写兜底数据时，同时写一个 `trending:meta:{dim}:{window}:fallback` 标志（string 0/1）
2. 读路径读该标志，响应加 `is_fallback: bool`
3. 即从「方案 A」演进到「方案 C 的轻量子集」（仅加标志，不拆 ZSET）

本次不做，保持读路径零改动。

---

## 十、已知权衡与风险

| 点 | 处理 |
|---|---|
| 全局热门老爆款霸榜 | 仅在窗口空时触发；此时本就没近期内容，属可接受退化。`active-circles-design.md §一` 已论证累积 hot 的霸榜问题，但本方案是「空」的兜底，非主路径 |
| job 层兜底无法前端标识 | 明确接受的取舍（§2.1）；前端展示一致性优先（兜底数据也是合法热门内容） |
| 兜底与窗口数据语义混合 | 不会混合：严格「仅空才兜底」，ZSET 非此即彼（§3.2） |
| 兜底查询增加 ES 负载 | 仅在窗口空时多一次聚合；空窗口本就数据少，兜底聚合（无窗口）一次 size:0/100，开销可控 |
| 兜底结果与上轮窗口数据跳变 | 窗口从「有数据」变「空」时，ZSET 内容会从窗口数据切到全局数据，前端可见跳变。属正常（窗口语义变化），下个刷新周期收敛 |

---

## 十一、分阶段交付

| 阶段 | 内容 | 验收 |
|---|---|---|
| **S1 ES 无窗口聚合** | `AggregateTrending` 支持 `window=""`；单测覆盖无窗口 filter 不含 range | 无窗口查询体无 `range create_time`；窗口查询不变 |
| **S2 syncer 兜底分支** | `syncOne` 增加窗口空 → 无窗口兜底；日志区分「窗口命中/兜底命中/全空」 | 窗口空时 ZSET 有兜底数据；窗口非空时不受影响 |
| **S3 联调验证** | 构造空窗口场景（如清空近 24h 帖子）→ 验证 24h 榜单有全局热门兜底；恢复窗口数据 → 验证回切窗口数据 | 24h 空时返回全局热门；窗口有数据时返回窗口数据 |

---

## 十二、明确不做（边界）

- ❌ **不做稀疏兜底**（< N 条补足）——语义混合 + 去重复杂度（§3.2）
- ❌ **不新增 ZSET / Redis key**——兜底写同一个 `trending:{dim}:{window}`
- ❌ **不改读路径 / DTO / 配置**——兜底对前端透明
- ❌ **不做扩窗兜底**（24h 空 → 30d）——窗口语义混乱（§2.2）
- ❌ **不做 latest 兜底**——三类榜单逻辑要另写（§2.2）
- ❌ **不做 fallback 标志透出**——job 层兜底的固有取舍（§9.1）；后续按需演进

---

## 附录：与原设计的关系

| 文档 | 关系 |
|---|---|
| [trending-design.md](trending-design.md) | **基线**：本方案是其增量优化，不推翻原架构，仅补足「空窗口」场景 |
| [hot-sync-design.md](hot-sync-design.md) | **上游**：兜底信号（全局 hot）来自此子系统产出 |
| [active-circles-design.md](active-circles-design.md) | **参照**：其 §一 论证了累积 hot 霸榜问题，本方案在「仅空才兜底」约束下规避了主路径风险 |

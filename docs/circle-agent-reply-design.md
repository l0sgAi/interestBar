# 圈内机器人回复触发与限流应用设计（circle-agent-reply）

> 目标：圈子级机器人（`ai_agent.circle_id` 非 NULL）在**本圈内**真正具备回复能力——
> 评论关键词触发、发帖 @提及 触发、圈内手动触发三条入口全部按 `post.circle_id == agent.circle_id`
> 匹配生效；限流配置（`max_replies_per_hour` / `min_interval_sec`）按机器人维度正常应用。
> 基线：
> ① 圈内机器人 CRUD 已落地（`/circle/agent/*`，[circle-agent-manage-design.md](circle-agent-manage-design.md)）；
> ② 触发侧防泄漏护栏已生效（`ListEnabled` 只加载全局机器人 + `ManualReply` 拒绝圈内，P1 承诺"届时移除/改造守卫"）；
> ③ @提及 可见范围已按圈收口（[circle-agent-mention-scope-design.md](circle-agent-mention-scope-design.md)，本期落地）；
> ④ 回复执行链（两阶段分类器+生成器、并发信号量、reply_log 审计）在全局机器人上运行稳定。
> 本文所有 file:line 基于当前 develop 分支，动手前先 grep 校准。

---

## 一、现状盘点（新开发者必读）

| 资产 | 位置 | 说明 |
|---|---|---|
| 关键词触发入口 | [reply_service.go:201](../pkg/domains/aiagent/application/reply_service.go#L201) | `OnCommentCreated`：防回环 → `ListEnabled`（**仅全局**）→ mode=2 + 关键词命中 → 并发信号量 → `executeReply` |
| @提及触发入口 | [reply_service.go:283](../pkg/domains/aiagent/application/reply_service.go#L283) | `OnPostMentioned`：`ListEnabled` → 按 `linked_user_id ∈ mention 名单` 命中 → `executeReply`（不校验 mode/关键词） |
| 手动触发入口 | [reply_service.go:347](../pkg/domains/aiagent/application/reply_service.go#L347) | `ManualReply`：role=1 → `agent.CircleID != nil` 直接 `errCircleReplyUnsupported`（[reply_service.go:355](../pkg/domains/aiagent/application/reply_service.go#L355)，护栏②） |
| 防泄漏护栏 | [agent_repo_pg.go:197](../pkg/domains/aiagent/infrastructure/agent_repo_pg.go#L197) | `ListEnabled` 带 `circle_id IS NULL`——圈内机器人不进任何触发链 |
| 限流应用点 | [reply_service.go:407](../pkg/domains/aiagent/application/reply_service.go#L407) | `executeReply` 内 per-agent：最近 1h 日志行数（`CountSinceByAgent`）+ 距最新一条间隔（`GetLastByAgent`，[reply_log.go:49](../pkg/domains/aiagent/domain/reply_log.go#L49)） |
| 评论事件载荷 | [reply_service.go:76](../pkg/domains/aiagent/application/reply_service.go#L76) | `CommentEvent{CommentID, PostID, UserID, RootID, Content}`——**无圈子ID**；comment 侧触发端口签名 [comment/service.go:72](../pkg/domains/comment/application/service.go#L72) 同样只传 postID |
| 发帖事件载荷 | [reply_service.go:84](../pkg/domains/aiagent/application/reply_service.go#L84) | `PostMentionEvent{PostID, UserID, MentionUserIDs}`——**无圈子ID**；post 侧触发端口 [post/service.go:91](../pkg/domains/post/application/service.go#L91) 同样 |
| 圈子ID已在源头 | comment/service.go:327、post CreatePost | comment 的 `CreateComment` 已拿到 `post.CircleID`（上期 `PostInfo.CircleID`），post 的 `CreatePost` 手头即 `input.CircleID`——**只差往回调里传** |
| 机器人侧帖子摘要 | [reply_service.go:25](../pkg/domains/aiagent/application/reply_service.go#L25) | aiagent 的 `PostBrief` **无 CircleID**；post 域 `GetPostBrief` DTO（[post/service.go:286](../pkg/domains/post/application/service.go#L286)）同样无——手动触发做跨圈校验需要补 |
| 圈主权限端口 | [circle_service.go:35](../pkg/domains/aiagent/application/circle_service.go#L35) | `CircleRoleReader.GetCircleMembership` 已存在（含 composition 桥 `circleRoleReaderForAgent`），ReplyService 未接 |
| 并发上限 | [reply_service.go:122](../pkg/domains/aiagent/application/reply_service.go#L122) | 全局共享信号量 `reply_concurrency`（默认 3），候选机器人增多后共同竞争 |
| mode=1 全部新帖 | [agent.go:67](../pkg/domains/aiagent/domain/agent.go#L67) | `TriggerModeAllPost` 无任何钩子（agent-reply P2 待实现）——圈内版同样不做 |

**关键既有结论**（勿重新论证）：限流 per-agent 口径对圈内机器人天然成立（机器人唯一属圈，
无需按圈聚合）；`0=不限` 是 API 文档既定语义（[circle-agent-manage-api.md:71](circle-agent-manage-api.md)）；
`UpdateCircleAgent` 已可调限流字段（运营字段 admin+，[circle_service.go:219](../pkg/domains/aiagent/application/circle_service.go#L219)）；
comment 触发回调在帖子已发布校验之后（`post.CircleID` 必非 Nil）；
`ExistsByLinkedUserID` 按全表 linked_user_id 防回环，圈内机器人自动覆盖。

---

## 二、方案

### 2.1 作用域匹配规则（唯一核心规则，三条入口共用）

```
agentInScope(agent, postCircleID):
  agent.CircleID == nil                    → true   // 全局机器人全站回复（现状保留）
  agent.CircleID != nil
    && postCircleID != uuid.Nil
    && *agent.CircleID == postCircleID     → true   // 本圈机器人本圈帖
  其余                                     → false  // 他圈帖 / 圈子未知（fail-closed）
```

- **fail-closed**：`postCircleID == Nil`（理论不可达，防御性）时圈内机器人一律不触发。
- 全局机器人行为零变化（含在圈内帖回复——现状语义，调整属 P2 圈子级开关）。

### 2.2 入口一：评论关键词触发

- 事件载荷加圈子：`CommentEvent` 加 `PostCircleID uuid.UUID`；comment 域端口
  `AgentReplyTrigger.OnCommentCreated` 签名加 `postCircleID uuid.UUID`（调用点
  comment/service.go:327 手头已有 `post.CircleID`）；桥接器 `commentAgentTrigger` 透传。
- **候选集查询收口**：repo 新方法
  `ListEnabledForCircle(ctx, circleID) → WHERE deleted=0 AND status=1 AND (circle_id IS NULL OR circle_id = ?)`
  （circleID=Nil 退化为仅全局）。替代 `OnCommentCreated` 现在的 `ListEnabled`——
  不加载全平台圈内机器人，每条评论的候选集 = 全局机器人 + 本圈 ≤5 个。
- `OnCommentCreated` 拿到候选后逐个：mode=2 → 关键词命中 → 并发槽 → `executeReply`（不变）。
  圈内机器人与全局机器人走完全相同的执行链（分类器 filter_prompt / 限流 / 楼层挂载）。

### 2.3 入口二：发帖 @提及 触发

- `PostMentionEvent` 加 `PostCircleID`；post 域端口 `AgentPostTrigger.OnPostMentioned`
  签名加 `circleID`（`CreatePost` 手头即 `input.CircleID`）；桥接器 `postAgentTrigger` 透传。
- 候选集同样改用 `ListEnabledForCircle(evt.PostCircleID)`，mention 命中（`linked_user_id ∈ 名单`）
  即触发（不校验 mode 的既有语义保留）。
- 第二道防线：上期 mention 兜底已在落库名单中剔除越圈机器人，此处 scope 匹配再挡一次
  （防构造请求、防投影列漂移），双层一致。

### 2.4 入口三：手动触发（新增圈内版）

`ReplyService` 新增方法（service 层统一鉴权，与 CircleAgentService 同范式）：

```go
// CircleManualReply 圈内手动触发回复（同步，仅该圈圈主；trigger_mode=3 的启用圈内机器人）。
// 校验链：load 圈内机器人（全局机器人/不存在 → 404）→ requireCircleOwner（403）→
// enabled → mode=3 → 帖子门槛（已发布未锁定）+ post.CircleID == agent.CircleID（跨圈 → 404）
// → executeReply（限流/两阶段链路同全局）。返回生成的评论 ID。
CircleManualReply(ctx context.Context, operatorID, agentID, postID uuid.UUID) (uuid.UUID, error)
```

- **权限仅圈主**（role=30）：手动触发直烧计费凭据（api_key 由圈主持有，字段分级先例）。
  `ReplyService` 加 `SetCircleRoleReader`（复用既有 `CircleRoleReader` 端口与桥接器）。
- **跨圈校验**：`post.CircleID != *agent.CircleID → errPostNotInAgentCircle`（新哨兵，
  handler 映射 404——不暴露它圈帖子与机器人的存在性）。
- 前置依赖：`PostBrief`（aiagent 侧 [reply_service.go:25](../pkg/domains/aiagent/application/reply_service.go#L25)
  与 post 侧 `GetPostBrief` DTO）补 `CircleID uuid.UUID` 字段，`agentPostReader` 桥回填
  （post 实体本就有该列，纯透传）。
- 路由：`POST /circle/agent/:id/reply/:postId`（`CircleAgentHandler` 构造器加 replySvc）。

### 2.5 全局 `ManualReply` 对齐

现为 `errCircleReplyUnsupported`（403 风格），与全局链其余方法的"跨作用域不可见=404"
惯例（`loadGlobalAgent`）不一致。改为 `errAgentNotFound`（404）：全局控制台本就不展示
圈内机器人，404 不泄漏存在性；`errCircleReplyUnsupported` 哨兵随之删除（唯一使用点）。

### 2.6 限流配置应用（本期核心验证项，无结构改造）

| 项 | 结论 |
|---|---|
| per-agent 限流 | `executeReply` 既有逻辑对圈内机器人**原样生效**（机器人唯一属圈，per-agent 即 per-圈-内单机器人），零改动 |
| 配置修改链路 | `UpdateCircleAgent` 已支持（运营字段 admin+ 可改 max/min），零改动 |
| 创建默认值 | **决策项 B**：`validateAndBuildAgent` 现状原样透传——创建时不传即 `0=不限`。圈内机器人的 key 由圈主付费，裸奔不限速有真实烧钱风险。建议：`CreateCircleAgent` 在构建后补默认（`MaxRepliesPerHour<=0 → 30`，`MinIntervalSec<=0 → 60`，DDL 同款默认值），全局链路不动；代价是圈内创建无法表达"不限"，需先手动改小再改 0（更新接口仍可设 0） |
| 并发信号量 | 候选集含圈内机器人后共享 `reply_concurrency`（默认 3）。本期不改代码，部署时按平台机器人总量 review 配置（每圈 ≤5 × 圈数 + 全局数） |
| 审计 | `reply_log` 不加圈子列：agent→circle 可 join 查，维度需求出现时再演进 |

### 2.7 `ListEnabled` 旧方法归宿

两个触发入口均改用 `ListEnabledForCircle` 后，`ListEnabled`（含护栏）无调用方 →
**删除**（domain 接口 + PG 实现 + 文档引用）。护栏使命由"新方法 OR 语义 + 触发链 scope
匹配"接棒——想全站触发的旧路径不复存在。

---

## 三、数据流

### 评论关键词触发（圈内机器人首次进入）

```
评论创建 ──► comment.CreateComment（post.CircleID 已在手）
  │ agentTrigger.OnCommentCreated(postID, postCircleID, ...)
  ▼
ReplyService.OnCommentCreated(evt{...PostCircleID})
  ├─ 防回环: ExistsByLinkedUserID(evt.UserID) → 是机器人则返回
  ├─ ListEnabledForCircle(evt.PostCircleID)   ← 全局机器人 + 本圈 ≤5
  └─ 逐个: mode=2 → 关键词命中 → sem 槽位 → goroutine:
       executeReply: 帖子门槛 → per-agent 限流（max/h + min_interval）
       → 分类器(可选 filter_prompt) → LLM 生成 → 挂触发评论楼层落库 → reply_log
```

### 发帖 @提及 触发

```
发帖 ──► post.CreatePost（input.CircleID 在手）
  │ agentTrigger.OnPostMentioned(postID, input.CircleID, authorID, mentionIDs)
  ▼
ReplyService.OnPostMentioned(evt{...PostCircleID})
  ├─ ListEnabledForCircle(evt.PostCircleID)
  └─ 逐个: linked_user_id ∈ mention 名单 → sem 槽位 → executeReply(trigger=nil 顶层评论)
       （mention 兜底已剔除越圈机器人，scope 匹配为第二道防线）
```

### 圈内手动触发

```
圈主 ──► POST /circle/agent/:id/reply/:postId
  ▼
CircleManualReply: loadCircleAgent(404) → requireCircleOwner(403)
  → enabled → mode=3 → GetPostBrief: 已发布未锁定 且 CircleID==agent.CircleID(否则404)
  → executeReply（限流同全局）
```

---

## 四、Schema / 配置变更

**无 DDL、无新配置项、无缓存 key。** 全部为接口签名与逻辑变更：

| 变更 | 文件 |
|---|---|
| comment 触发端口签名加 postCircleID | comment/application/service.go（接口 + :327 调用点） |
| post 触发端口签名加 circleID | post/application/service.go（接口 + CreatePost 调用点） |
| `CommentEvent`/`PostMentionEvent` 加 `PostCircleID` | aiagent/application/reply_service.go |
| repo：新增 `ListEnabledForCircle`、删除 `ListEnabled` | aiagent domain/repository.go + infrastructure/agent_repo_pg.go |
| `ReplyService` 加 `CircleManualReply` + `SetCircleRoleReader`；`ManualReply` 圈内→404 | aiagent/application/reply_service.go + errors.go |
| `PostBrief` 加 CircleID（aiagent 侧 + post 侧 DTO） | 两域 application + composition/agent_bridges.go 桥 |
| 路由 + handler | aiagent/interfaces/http/routes.go、handler.go |
| 装配 | composition/server.go（replySvc.SetCircleRoleReader、CircleAgentHandler 加参） |

---

## 五、一致性 / 边界 / 风险

| 主题 | 决策 | 备注 |
|---|---|---|
| 护栏移除后的泄漏窗口 | 新方法 OR 语义 + 触发链 `agentInScope` 双保险；Nil fail-closed | 单测覆盖他圈/Nil 不触发；上线顺序无中间态（同版本内完成） |
| 全局机器人仍全站回复 | 现状保留（含圈内帖） | 圈子级"禁用全局机器人"开关属 P2，本期不做 |
| 回环 | `ExistsByLinkedUserID` 全表口径已覆盖圈内机器人 | 机器人评论（含圈内）不触发任何机器人，现状语义 |
| 限流裸奔风险 | 决策项 B（建议圈内创建补默认 30/60） | 更新接口仍可显式设 0=不限 |
| 并发槽竞争 | 不改代码，部署 review `reply_concurrency` | 候选总量 = 全局 + 每圈 ≤5；信号量满则跳过本轮（尽力而为语义不变） |
| 跨圈手动触发 | `errPostNotInAgentCircle` → 404 | 圈主不能拿本圈机器人刷它圈帖子 |
| mention 名单漂移 | scope 匹配是第二道防线 | 兜底剔除（上期）在名单层，此处触发层，互不依赖 |
| mode=1 全部新帖 | 无钩子，本期不做（全局/圈内同不做） | P2 与全局一起设计 |
| reply_log 圈维度审计 | 不加列，agent→circle join 可查 | 需求出现再演进 |

---

## 六、分阶段交付

### P0（本期）

| # | 交付物 | 位置 |
|---|---|---|
| 1 | repo `ListEnabledForCircle` + 删 `ListEnabled` | aiagent domain + infrastructure |
| 2 | 事件载荷与端口签名（comment/post 两域 + 桥） | comment、post、composition/agent_bridges.go |
| 3 | 触发链 scope 接入（两条入口改新方法） | reply_service.go |
| 4 | `PostBrief.CircleID` 两域 + 桥 | aiagent、post、composition |
| 5 | `CircleManualReply` + `ManualReply` 404 对齐 + 新哨兵 | reply_service.go、errors.go |
| 6 | 路由 `POST /circle/agent/:id/reply/:postId` + handler | interfaces/http |
| 7 | 决策项 B 落地（圈内创建限流默认值） | circle_service.go |
| 8 | 装配（SetCircleRoleReader 等） | composition/server.go |
| 9 | 单测：scope 匹配（全局/本圈/他圈/Nil）、触发链候选集、手动触发权限矩阵 + 跨圈 404、限流对圈内 agent 生效、404 对齐回归 | 各层 _test.go |
| 10 | API 文档：手动触发端点 + 触发行为说明 | circle-agent-manage-api.md |

### P1 / P2（后续，非本期）

| 项 | 说明 |
|---|---|
| mode=1 全部新帖触发 | 全局 + 圈内一起设计（post 创建钩子 + 限流强化） |
| 圈子级"禁用全局机器人"开关 | 圈主可屏蔽全局机器人在本圈回复 |
| reply_log 圈维度审计列/视图 | 运维需求出现再做 |
| prompt 带圈子上下文（圈名/简介） | 提升圈内回复相关性，纯增强 |

---

## 七、明确非目标（本期不做）

- ❌ 全局机器人可见/触发范围调整（仍全站回复）
- ❌ `TriggerModeAllPost`（mode=1）钩子——任何链路都不做
- ❌ 限流口径改造（per-agent 已天然按圈隔离，不动结构）
- ❌ reply_log schema 变更

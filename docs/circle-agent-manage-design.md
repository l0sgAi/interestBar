# 圈子级 AI 代理管理（绑定 + CRUD）设计文档

> 目标：圈主（owner）/ 管理员（admin）可管理**本圈**的 AI 回复机器人——增删改查全套 CRUD，
> 每圈上限 5 个。本期**不含回复链路改造**（圈内机器人暂不参与任何触发，见 §4.3 防泄漏护栏）。
> 基线：
> ① 全局机器人管理已落地（`/agent/*`，users.role=1 超管专属，见 [agent-reply-design.md](agent-reply-design.md)）；
> ② Phase 1「我可管理的圈子列表」`GET /circle/manage/list` 已交付（契约见 [circle-manage-list-api.md](circle-manage-list-api.md)），
> 列表项 `agent_count` 目前恒 0，本期回填真实值；
> ③ 圈内权限校验范式已有 `requireManageRole`（[manage.go:165](../pkg/domains/circle/application/manage.go#L165)）。
> 本文所有 file:line 基于当前 develop 分支，动手前先 grep 校准。

---

## 一、现状盘点（新开发者必读）

| 资产 | 位置 | 说明 |
|---|---|---|
| 机器人聚合根 | [agent.go:18](../pkg/domains/aiagent/domain/agent.go#L18) | `AiAgent` **无 circle_id**——当前全部机器人都是平台全局 |
| 全局管理 Service | [service.go:134](../pkg/domains/aiagent/application/service.go#L134) | `AgentService` 五方法（Create/Get/List/Update/Delete），每个入口先过 `ensureAdmin`（users.role=1，fail-closed） |
| 校验函数组 | [service.go:569-649](../pkg/domains/aiagent/application/service.go#L569) | validateName/Protocol/Trigger/LLMParams/RateLimit/FilterPrompt/Status——本期圈内 CRUD **全部复用** |
| 机器人仓储 | [repository.go:17](../pkg/domains/aiagent/domain/repository.go#L17) + [agent_repo_pg.go](../pkg/domains/aiagent/infrastructure/agent_repo_pg.go) | Create/GetByID/ExistsByName/ListByOffset/UpdateFields/SoftDelete/ListEnabled/ExistsByLinkedUserID |
| 全局路由 | [routes.go:18](../pkg/domains/aiagent/interfaces/http/routes.go#L18) | `/agent/*` 全挂 authCheck，role=1 校验在 service 层——**本组路由不动** |
| 跨域桥接范式 | [agent_bridges.go](../pkg/composition/agent_bridges.go) | aiagent 声明端口（RoleReader/BotUserCreator/...），composition 写桥接器，setter 注入；`agentBotUserCreator` 有直持 *gorm.DB 的过渡先例 |
| 圈内角色常量 | [circle.go:76-80](../pkg/domains/circle/domain/circle.go#L76) | MemberRoleMember=10 / Admin=20 / Owner=30 |
| 圈内权限校验 | [manage.go:165](../pkg/domains/circle/application/manage.go#L165) | `requireManageRole`：角色下限 + 操作者须 normal；非成员视同无权限（不暴露成员身份） |
| 分字段权限先例 | [manage.go:146](../pkg/domains/circle/application/manage.go#L146) | `UpdateCircleProfile`：name/slug/join_type/category_id 仅圈主，avatar/description 等 admin+——本期字段分级的直接参照 |
| Phase 1 列表 | [manage.go:73](../pkg/domains/circle/application/manage.go#L73) | `ListManagedCircles` 直查 PG 不走缓存/ES；`AgentCount` 暂硬编码 0（[manage.go:101](../pkg/domains/circle/application/manage.go#L101)） |
| 名称唯一索引 | [ai-agent.md:78](../docs/pgsql-ddl/ai-agent.md#L78) | `idx_ai_agent_name ON (name) WHERE deleted = 0`——**部分唯一索引**，本期重定作用域 |
| 回复触发链 | [reply_service.go:94](../pkg/domains/aiagent/application/reply_service.go#L94) | 关键词/手动/@提及三条入口均加载**全量**启用机器人（`ListEnabled`），无圈子概念——本期必须加护栏（§4.3） |

**关键既有结论**（勿重新论证）：机器人发评论以 `linked_user_id`（role=2 系统用户）身份，创建 agent 时同步建 users 行；
api_key AES-GCM 加密存密文、任何接口不回显明文（`toVO` 只回掩码）；
机器人资料改名/换头像经 `BotUserProfileUpdater` 同步 users 表，失败转异步重试（[service.go:483](../pkg/domains/aiagent/application/service.go#L483)）。

---

## 二、权限模型设计（owner vs admin）

### 2.1 操作权限矩阵

| 操作 | 圈主 (30) | 管理员 (20) | 理由 |
|---|---|---|---|
| 圈内机器人列表 / 详情 | ✅ | ✅ | 两者都是运营者；api_key 恒回掩码（既有 toVO 行为不变） |
| 创建机器人 | ✅ | ✅ | 共享同一 ≤5 配额 |
| 更新**运营字段**：name / avatar_url / model / llm_params / system_prompt / filter_prompt / trigger_mode / trigger_keywords / max_replies_per_hour / min_interval_sec / status | ✅ | ✅ | 日常调优 |
| 更新**凭据字段**：api_protocol / base_url / api_key | ✅ | ❌ 403 | 计费凭据由圈主持有；直接对齐 UpdateCircleProfile「身份字段仅圈主」先例 |
| 删除机器人（软删） | ✅ | ❌ 403 | 破坏性操作仅圈主，对齐转让/任免的 owner-only 惯例 |

### 2.2 通用规则（与 circle 管理域完全同构）

1. **成员状态门槛**：操作者须为圈内 `status=normal` 成员——被禁言/拉黑/待审/已退出的 owner/admin **管理权暂停**，恢复后自动生效（复用 `requireManageRole` 语义，不新造规则）。
2. **非成员 = 无权限**：返回 403，不暴露"你不是成员"的细节（防成员身份探测）。
3. **平台超管（users.role=1）不特殊处理**：本期对圈内机器人**无读写特权**；其控制台仍是 `/agent/*`（全局机器人）。超管对圈内机器人的只读审计视图延后（见 §八 P1）。
4. **校验位置**：全部在 service 层（`CircleAgentService`），handler 只 bind + 调 service + 映射错误——与 `AgentService` 注释条款一致（"防止新增入口漏检"）。
5. **跨作用域不可见**：圈内机器人 ID 拿到 `/agent/:id`（全局链）查询 → 404；全局机器人 ID 拿到 `/circle/agent/:id` → 404。两链路互不暴露对方存在性。

### 2.3 与 circle 域既有权限的关系

圈内机器人管理**不引入新角色、不改 member 表**，完全复用 `circle_member.role/status`。
角色变更（任免/转让/禁言）对机器人管理权**即时生效**——权限校验每次直查 member 记录，无缓存。

---

## 三、数据模型与 DDL 变更

### 3.1 `domains.ai_agent` 新增两列

```sql
ALTER TABLE domains.ai_agent ADD COLUMN circle_id uuid NULL;   -- 绑定圈子ID, NULL=平台全局机器人
ALTER TABLE domains.ai_agent ADD COLUMN creator_id uuid NULL;  -- 创建者用户ID(审计; 全局/圈内均记录)
COMMENT ON COLUMN domains.ai_agent.circle_id IS '绑定圈子ID(domains.circle.id,无FK应用层保证);NULL=平台全局机器人(超管维护)';
COMMENT ON COLUMN domains.ai_agent.creator_id IS '创建者用户ID(users.id,审计用)';
```

- `circle_id` **创建后不可变**：不提供"全局↔圈内"迁移接口（本期明确不支持，避免配额/权限语义混乱）。
- 存量行全部 `circle_id = NULL` → 保持全局语义，**零行为变化**。
- `creator_id` 对存量行回填 NULL（无法追溯），新行必写。

### 3.2 名称唯一索引重定作用域

现状：`idx_ai_agent_name ON (name) WHERE deleted = 0`（全局唯一）。
问题：两个圈都想叫"小助手"会被全局唯一挡住，圈内场景不可接受。

```sql
-- 已建表的存量库由 DB-owner 执行：
DROP INDEX IF EXISTS domains.idx_ai_agent_name;
CREATE UNIQUE INDEX idx_ai_agent_name ON domains.ai_agent
    (COALESCE(circle_id, '00000000-0000-0000-0000-000000000000'::uuid), name)
    WHERE deleted = 0;

-- 圈内列表/计数：
CREATE INDEX idx_ai_agent_circle ON domains.ai_agent (circle_id) WHERE deleted = 0;
```

- 全局机器人共享全零 UUID 桶 → **全局唯一语义原样保留**；圈内按各自 circle_id 桶唯一。
- 迁移安全性：存量名称本来全局唯一，新索引（全局桶, name）不会撞——直接可建。
- `ExistsByName` 相应加 circle 作用域参数（§4.1）。

DDL 权威来源更新进 [ai-agent.md](../docs/pgsql-ddl/ai-agent.md)（建表语句 + 存量迁移段，格式对齐该文件既有"存量表迁移"节），由 DB-owner 执行。

### 3.3 GORM 实体变更（[agent.go:18](../pkg/domains/aiagent/domain/agent.go#L18)）

```go
CircleID  *uuid.UUID `json:"circle_id,omitempty" gorm:"column:circle_id;type:uuid"`   // nil=平台全局
CreatorID uuid.UUID  `json:"creator_id" gorm:"column:creator_id;type:uuid"`              // 审计
```

领域常量新增：`MaxAgentsPerCircle = 5`（本期硬编码；如需运营可调再提升为 conf 配置项）。

---

## 四、分层实施设计

### 4.1 domain 层（[repository.go](../pkg/domains/aiagent/domain/repository.go)）

`AgentRepository` 新增：

```go
// ExistsByNameInScope 检查 (circle 作用域, name) 是否被占用；circleID=uuid.Nil 表示全局桶。
// 替代原 ExistsByName（签名变更，全局调用方传 uuid.Nil）。
ExistsByNameInScope(ctx context.Context, circleID uuid.UUID, name string, excludeID uuid.UUID) (bool, error)
// CountByCircleIDs 批量统计各圈未删除机器人数（Phase 1 列表 agent_count 回填用）。
CountByCircleIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]int, error)
// ListByCircle 圈内机器人 offset 分页（未删除，create_time DESC）；
// keyword 非空按 name ILIKE %kw% 过滤，语义对齐 ListByOffset。
ListByCircle(ctx context.Context, circleID uuid.UUID, keyword string, offset, limit int) ([]AiAgent, int64, error)
// CreateInCircle 单事务：SELECT circle 行 FOR UPDATE（串行化同圈创建）→
// 计数 >= maxPerCircle 返回 ErrCircleAgentLimit → 插入。
// 撞 idx_ai_agent_name 由 DB 兜底（调用方已 ExistsByNameInScope 预检）。
CreateInCircle(ctx context.Context, agent *AiAgent, maxPerCircle int) error
```

哨兵错误新增（domain 层）：`ErrCircleAgentLimit = errors.New("circle agent limit exceeded")`。

**为什么用行锁而不是 advisory lock / 先查后插**：PG 无"每组行数上限"约束；两个并发创建会同时通过
`count < 5` 预检。事务内 `SELECT id FROM domains.circle WHERE id = ? FOR UPDATE` 把同圈创建串行化，
代价一行锁、无哈希碰撞、无语义化锁键管理。圈行本身存续期长（软删不物理删行），锁目标稳定。

### 4.2 application 层（aiagent）

#### 新 Service：`CircleAgentService`（新文件 `application/circle_service.go`，不碰 `AgentService`）

```go
type CircleAgentService interface {
    CreateCircleAgent(ctx, operatorID, circleID uuid.UUID, input CreateAgentInput) (*AgentVO, error)
    GetCircleAgent(ctx, operatorID, agentID uuid.UUID) (*AgentVO, error)
    ListCircleAgents(ctx, operatorID, circleID uuid.UUID, keyword string, page, size int) (*AgentListResult, error)
    UpdateCircleAgent(ctx, operatorID, agentID uuid.UUID, input UpdateAgentInput) (*AgentVO, error)
    DeleteCircleAgent(ctx, operatorID, agentID uuid.UUID) error

    SetCircleRoleReader(r CircleRoleReader)          // 跨域权限端口（composition 桥接）
    SetBotUserCreator(c BotUserCreator)              // 复用现有端口
    SetBotUserProfileUpdater(u BotUserProfileUpdater)
}
```

#### 跨域端口（声明在 aiagent/application，与 RoleReader 同范式）

```go
// CircleRoleReader 跨域端口：读取用户在圈内的角色/成员状态。
type CircleRoleReader interface {
    // GetCircleMembership 返回 (role, status, isMember)；非成员/圈子不存在 ok=false。
    GetCircleMembership(ctx context.Context, circleID, userID uuid.UUID) (role, status int16, ok bool, err error)
}
```

#### 权限助手（fail-closed：端口未注入一律拒绝）

```go
// requireCircleManager：role >= 20(admin) 且 status=normal；否则 errNotCircleAdmin。
// requireCircleOwner：role == 30 且 status=normal；否则 errNotCircleOwner。
// loadCircleAgent：repo.GetByID → agent.CircleID == nil → errAgentNotFound（404，不暴露全局机器人）。
```

#### 各方法要点

- **Create**：`requireCircleManager` → 复用全部 validateXxx → `ExistsByNameInScope(circleID, name, Nil)` 预检 →
  建机器人系统用户（复用 BotUserCreator，email 仍 uuid 派生全局唯一）→
  `agent.CircleID = &circleID; agent.CreatorID = operatorID` → `repo.CreateInCircle`（限额 409）。
- **List**：`requireCircleManager(circleID)` → `ListByCircle`，page/size 规整同 `ListAgents`（[service.go:312-317](../pkg/domains/aiagent/application/service.go#L312)）。
- **Get**：`loadCircleAgent` → `requireCircleManager(*agent.CircleID)`。顺序固定先 404 后 403（不泄露归属）。
- **Update**：`loadCircleAgent` → **字段分级**：input 的 APIProtocol/BaseURL/APIKey 任一非 nil → `requireCircleOwner`；否则 `requireCircleManager` → 复用 UpdateAgent 的字段校验/map 组装 → 名称变更走 `ExistsByNameInScope` → 改名/换头像同步 BotUserProfileUpdater（含异步重试，原样复用）。
- **Delete**：`loadCircleAgent` → `requireCircleOwner` → `repo.SoftDelete`（deleted=1 且停用，与全局一致）。
- **circle 状态不阻断管理**：圈子被封禁（status=2）时 owner/admin 仍可 list/get/update/delete 本圈机器人
  （需要能停用/清理），权限只看 member 记录。文档明示此语义。

#### DTO / 错误

- `AgentVO` 加 `CircleID *uuid.UUID \`json:"circle_id,omitempty"\``（nil 不回显）。
- errors.go 新增哨兵 + 谓词：`errNotCircleAdmin` / `errNotCircleOwner` / `errCircleAgentLimit`
  （`IsNotCircleAdminErr` 等导出谓词，格式对齐既有文件）。

### 4.3 infrastructure 层（[agent_repo_pg.go](../pkg/domains/aiagent/infrastructure/agent_repo_pg.go)）

- 实现 §4.1 四个新方法；`ExistsByNameInScope` 的 WHERE：`COALESCE(circle_id, 全零UUID) = COALESCE(?, 全零UUID)` 或分两支（circleID==Nil 走 `circle_id IS NULL`），与部分唯一索引对齐。
- **`ListEnabled` 加护栏**：`AND circle_id IS NULL`（[agent_repo_pg.go:95](../pkg/domains/aiagent/infrastructure/agent_repo_pg.go#L95)）。
  **这是本期必须的防泄漏改动**：回复触发链（关键词/手动/@提及）经 `ListEnabled` 加载全量启用机器人，
  不加此过滤，圈内机器人一创建就会在**全站**触发回复。一行改动，属本期范围，不属于回复链改造。
- **`ExistsByLinkedUserID` 保持不加 circle 过滤**：它的职责是防回环（机器人自己的评论不再触发），
  圈内机器人的 linked user 同样需要被识别——虽然本期圈内机器人不触发，但防回环口径应覆盖全部机器人，为未来留正确语义。
- `ManualReply` 链路（[reply_service.go](../pkg/domains/aiagent/application/reply_service.go)）补同样守卫：
  手动触发前校验 `agent.CircleID == nil`，圈内机器人返回不支持（防超管手动入口误触发圈内机器人全站回复）。

### 4.4 interfaces/http 层（aiagent）

[routes.go](../pkg/domains/aiagent/interfaces/http/routes.go) 新增独立路由组（`/agent` 组原样不动）：

```
POST   /circle/agent                 创建圈内机器人（admin+，≤5）
GET    /circle/agent/list            圈内列表（admin+；query: circle_id, keyword, page, size）
GET    /circle/agent/:id             详情（该圈 admin+）
PUT    /circle/agent/:id             更新（字段分级：凭据字段 owner-only）
DELETE /circle/agent/:id             软删（owner-only）
```

- 全挂 `authCheck`；handler 结构复刻既有 `Handler`（本地 `requireUserID`、BindQuery 用 `query:` tag、BindJSON）。
- `writeCircleAgentError` 映射：`IsNotCircleAdminErr/IsNotCircleOwnerErr` → `httputil.Forbidden`；
  `errCircleAgentLimit` → `httputil.Conflict`（"Circle agent limit reached (5)"）；
  名称冲突 → `Conflict`；`ErrAgentNotFound` → `NotFound`；参数校验错 → `BadRequest`；未知 → `InternalError`。
- Create/List 请求 DTO 复用全局 agent 的字段定义（`CreateAgentRequest` 等），仅多 `circle_id`。

### 4.5 composition 装配（[agent_bridges.go](../pkg/composition/agent_bridges.go)）

| 桥接器 | 端口 → 实现 | 备注 |
|---|---|---|
| `circleRoleReader` | `CircleRoleReader` → circle 域 | **推荐**：circle 域 application 暴露 Facade 方法 `GetMemberRole(ctx, circleID, userID) (role, status int16, ok bool, err error)`（内部走 `memberRepo.GetMember`，含惰性解禁自愈），桥接器薄转发。备选：直持 *gorm.DB 查 circle_member（有 agentBotUserCreator 先例），但绕过惰性解禁逻辑，不取 |
| `circleAgentCounter` | circle 域新端口 `CircleAgentCounter.CountByCircleIDs` → aiagent `AgentRepository.CountByCircleIDs` | 方向反转：circle 域声明端口，桥到 aiagent repo，setter 注入 CircleService |

`server.go` 装配：`NewCircleAgentService(repo)` → 三个 setter → `RegisterRoutes` 增加参数传入。

### 4.6 circle 域 touch-up：agent_count 回填

[manage.go:101](../pkg/domains/circle/application/manage.go#L101) 的 `AgentCount: 0` 硬编码替换为：
列表查出后收集 circleIDs → `agentCounter.CountByCircleIDs`（端口 nil 或出错时降级为 0，不阻断列表）→ 逐项回填。
**仅此一处** circle 域感知 aiagent，依赖方向仍为 composition 桥接，无包级 import。

---

## 五、API 契约摘要

统一信封 `{code, message, data}`；全需登录（satoken）。字段语义同全局 agent（见前端文档既有定义），
响应 VO 多 `circle_id`。

| 场景 | HTTP | 说明 |
|---|---|---|
| 成功 | 200 | 信封 code=200 |
| 未登录 / token 失效 | 401 | 同既有端点 |
| 参数非法（circle_id 非 UUID 等） | 400 | |
| 非该圈 admin+ / 凭据字段或删除非圈主 | 403 | message 区分 NotCircleAdmin / NotCircleOwner |
| 机器人不存在 / 跨作用域访问 | 404 | |
| 超 5 限额 / 圈内名称冲突 | 409 | |
| 服务端异常 | 500 | |

详细前端接入文档（含 TS 类型、字段表、示例）交付物：`docs/circle-agent-manage-api.md`，
格式对齐 [circle-manage-list-api.md](circle-manage-list-api.md)。

---

## 六、数据流

### 创建圈内机器人（含限额与并发控制）

```
owner/admin ──► POST /circle/agent {circle_id, name, model, api_key, ...}
  │ handler: bind + requireUserID
  ▼
CircleAgentService.CreateCircleAgent
  │ ① requireCircleManager ──► CircleRoleReader ──(composition 桥)──► circle.GetMemberRole
  │    （role>=20 且 status=normal，否则 403）
  │ ② validate* 全量校验（复用全局函数组） + SanitizeForPg
  │ ③ ExistsByNameInScope(circleID, name) 预检（409 名称冲突）
  │ ④ BotUserCreator.CreateBotUser（role=2 系统用户，email=uuid 派生）
  ▼
repo.CreateInCircle ── 单事务 ──► SELECT domains.circle FOR UPDATE（同圈串行）
                              ──► COUNT(ai_agent WHERE circle_id=? AND deleted=0) < 5，否则 409
                              ──► INSERT ai_agent（circle_id, creator_id 写入）
```

### 权限校验通用路径

```
任意 /circle/agent/* ──► service 层
  loadCircleAgent(agentID)：不存在或 circle_id=NULL → 404
  requireCircleManager/Owner(agent.circle_id, operator) → CircleRoleReader 直查 member 记录
  （无缓存：任免/转让/禁言即时生效）
```

---

## 七、一致性 / 边界 / 风险

| 主题 | 决策 | 备注 |
|---|---|---|
| ≤5 并发创建竞态 | 事务内锁 circle 行（FOR UPDATE）后计数 | 双并发只过一；无 advisory lock 哈希碰撞问题 |
| 圈内机器人泄漏进全局触发链 | `ListEnabled` + `ManualReply` 加 `circle_id IS NULL` 守卫 | **本期必做**；不做则圈内机器人全站回复 |
| 防回环口径 | `ExistsByLinkedUserID` 不加 circle 过滤 | 覆盖全部机器人，语义前瞻正确 |
| 名称唯一迁移 | 存量名全局唯一 → 新 (桶,name) 索引可直接建 | 无回填；DB-owner 执行 DROP+CREATE |
| 角色变更时效 | 权限直查 member 记录，零缓存 | 任免后下次请求即生效 |
| 被禁言的圈主 | 管理权暂停（status≠normal → 403） | 与 circle 管理域同规则；解禁自动恢复 |
| 圈子被封禁/审核中 | 不阻断圈内机器人管理 | owner 需要能停用/删除机器人止血 |
| 软删机器人占用名称 | 部分索引 `WHERE deleted=0` → 删除后名称可复用 | 与全局语义一致 |
| 机器人改名同步 users | 复用同步 + 异步重试（指数退避 ×3） | 重试耗尽人工回填，既有策略不变 |
| agent_count 准确性 | 实时 COUNT（非缓存计数器） | 表小 + 部分索引，成本低；失败降级 0 不阻断列表 |
| 超管审计圈内机器人 | 本期无入口 | P1 再评估（只读视图） |

---

## 八、分阶段交付

### P0（本期，circle agent CRUD 闭环）

| # | 交付物 | 位置 |
|---|---|---|
| 1 | DDL 变更段（两列 + 索引重定 + 部分索引） | [ai-agent.md](../docs/pgsql-ddl/ai-agent.md)（交 DB-owner） |
| 2 | 实体 + 领域常量 + 哨兵错误 | aiagent/domain/agent.go、repository.go |
| 3 | repo 四新方法 + `ListEnabled`/`ManualReply` 防泄漏守卫 | aiagent/infrastructure/agent_repo_pg.go、reply_service 校验 |
| 4 | `CircleAgentService` + 权限助手 + errors 谓词 | aiagent/application/circle_service.go、errors.go |
| 5 | circle 域 `GetMemberRole` Facade + `CircleAgentCounter` 端口 + agent_count 回填 | circle/application（facade、manage.go:101） |
| 6 | composition 两桥接器 + 装配 | composition/agent_bridges.go、server.go |
| 7 | 5 个端点 + handler + 错误映射 | aiagent/interfaces/http/ |
| 8 | 单测：权限矩阵（owner/admin/member/非成员/被禁言 owner × 各端点）、字段分级（admin 改 api_key → 403）、限额边界（5→409、并发双创建只过一）、跨作用域 404、名称作用域（两圈同名可建/同圈同名 409/全局+圈内同名可建） | 各层 _test.go |
| 9 | 前端接入文档 | docs/circle-agent-manage-api.md |
| 10 | 设计文档回链 | [agent-reply-design.md](agent-reply-design.md) 补"圈内机器人阶段注记" |

### P1（后续，非本期）

| 项 | 说明 |
|---|---|
| 圈内回复触发 | 触发链匹配 post.circle_id == agent.circle_id（含 ManualReply 圈内版）；届时移除/改造本期守卫 |
| 超管只读审计视图 | 平台超管查看圈内机器人（掩码） |
| 通知扇出 | 机器人创建/删除通知圈主（复用 notice 链路） |
| agent_count 缓存化 | 若 COUNT 成热路径再引入计数缓存 |

---

## 九、明确非目标（本期不做）

- ❌ 圈内机器人的回复触发（关键词/手动/@提及/全部新帖）——护栏保证其**完全不触发**
- ❌ 全局 ↔ 圈内机器人的互相迁移（circle_id 创建后不可变）
- ❌ 平台超管对圈内机器人的任何读写特权
- ❌ 圈内机器人配额运营化配置（硬编码 5；需要时再提 conf）

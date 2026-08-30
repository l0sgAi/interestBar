# 圈子管理 P1（通知扇出 / 操作审计 / 成员列表开放）交接设计文档

> 目标：承接 [circle-management-design.md](circle-management-design.md) 的 P1 交付项，给后续开发者一份**可直接照做的实施设计**：
> ① 管理操作的通知扇出（被禁言/拉黑/任免/转让/审核结果）；② 管理操作审计表 + 写审计；
> ③ 普通成员可见的成员列表（仅 normal 成员）；④ P0 实施中发现的 joined ZSET 重建一致性修复。
> 基线：P0 已全部交付（角色变更/禁言/拉黑/审核/资料编辑/成员列表，9 个端点），
> 通知链路已由 [notice-design.md](notice-design.md) 落地（topic `notification_events` + notice 读侧域）。
> 本文所有 file:line 基于当前 main 分支，动手前先 grep 校准。

---

## 一、P0 已交付盘点（新开发者必读）

| 资产 | 位置 | 说明 |
|---|---|---|
| 管理用例 + 权限矩阵 | [manage.go:70-109](../pkg/domains/circle/application/manage.go) | `requireManageRole`（角色下限+操作者须 normal）/ `loadManagedTarget`（只能管理严格下级） |
| 9 个 service 方法 | [manage.go:122-419](../pkg/domains/circle/application/manage.go) | ListCircleMembers/SetMemberRole/TransferOwner/MuteMember/UnmuteMember/BanMember/UnbanMember/ReviewJoinRequest/UpdateCircleProfile |
| 管理错误谓词 | [errors.go:27-56](../pkg/domains/circle/application/errors.go) | errNotCircleAdmin/errNotCircleOwner/errCannotManageTarget 等，handler 已映射 |
| repo 管理方法（CAS） | [circle_repo_pg.go:314-417](../pkg/domains/circle/infrastructure/circle_repo_pg.go) | ListMembers/UpdateMemberRole/UpdateMemberStatus/TransferOwner，状态迁移带 `WHERE status=旧值`，0 行 → `ErrMemberStateConflict`(409) |
| GetMember 惰性解禁 | [circle_repo_pg.go:181](../pkg/domains/circle/infrastructure/circle_repo_pg.go) | 过期禁言读时自愈回 normal；post 域发帖校验经此方法自动受益 |
| 计数副作用链路 | manage.go BanMember:267 / ReviewJoinRequest:331 | 拉黑 -1 / 审核通过 +1，复用 `decr/incrMemberCountWithRecovery` + `PublishMemberCount` + joined ZSET 增删，与 join/leave 完全一致 |
| HTTP 端点 | [handler.go:311-624](../pkg/domains/circle/interfaces/http/handler.go) + [routes.go:52-68](../pkg/domains/circle/interfaces/http/routes.go) | GET /circle/members、POST /circle/manage/*、PUT /circle/update，全挂 authCheck |
| 游标单测 | [cursor_test.go](../pkg/domains/circle/infrastructure/cursor_test.go) | 成员列表游标 round-trip + 8 种篡改场景 |

**关键既有结论**（勿重新论证）：禁言不触发计数（仍是成员）；解除拉黑落 status=4(left) 而非 1（防"偷偷回圈"）；
owner 只能通过转让放弃身份；ES 由 CDC 追，编辑资料只改 DB + 失效 `CircleBaseCache`。

### P1 要接入的通知触发点（P0 已留注释锚点）

P0 各管理方法成功后均有 `// (P1) notice 扇出` 语义的空缺（见 manage.go 顶部包注释），P1 即在此填充。
通知为 **best-effort**：发布失败仅记日志，不向主流程传播（对齐 [like_event_publisher.go:44-49](../pkg/domains/like/infrastructure/like_event_publisher.go) 的既有语义）。

---

## 二、P1 需求分析

### 需求 1：管理操作通知扇出（工作量最大，含 DDL + 前端联动）

| 操作（P0 方法） | 接收人 | actor | 通知内容语义 | 类型编码（建议） |
|---|---|---|---|---|
| MuteMember | 被禁言人 | 操作者 | 你在《圈子名》被禁言 N 小时 | 7 circle_muted |
| BanMember | 被拉黑人 | 操作者 | 你被《圈子名》移出并拉黑 | 8 circle_banned |
| SetMemberRole(→20) | 被任命人 | 圈主 | 你被任命为《圈子名》管理员 | 9 circle_admin_set |
| SetMemberRole(→10) | 被免职人 | 圈主 | 你已不是《圈子名》管理员 | 10 circle_admin_removed |
| TransferOwner ×2 | ①新圈主 ②旧圈主 | ①旧圈主 ②新圈主（**互换**，见 3.4） | 你已成为圈主 / 圈主已转让 | 11 circle_owner_transferred |
| ReviewJoinRequest(通过) | 申请人 | 操作者 | 你的《圈子名》加入申请已通过 | 12 circle_join_approved |
| ReviewJoinRequest(拒绝) | 申请人 | 操作者 | 你的《圈子名》加入申请未通过 | 13 circle_join_rejected |
| UnmuteMember / UnbanMember | — | — | **不通知**（对齐原设计操作集表） | — |

与既有 6 类通知的本质差异：**接收人在触发时已知**（就是 target_user_id），无需 consumer 反查解析；
目标从 post/comment 变为 **circle**（事件与表都需要 circle_id 才能跳转与去重）。

### 需求 2：管理操作审计表

合规追溯需求：谁（operator）在哪个圈（circle）对谁（target）做了什么（action），可带原因（reason）。
追加写入、只增不改不删。原设计文档 §5 已给表名 `circle_member_audit`，本期落地。

### 需求 3：普通成员可见的成员列表（normal only）

原权限矩阵：「查看成员列表(含 pending/拉黑)：owner ✅ / admin ✅ / 普通成员 ❌(仅见 normal 成员，P1)」。
即普通成员（role=10 且 status=1）可查看成员列表，但**只能看到 normal 状态成员**（看不到 pending/禁言/拉黑/退出名单）。

### 需求 4：joined ZSET 重建一致性修复（P0 实施发现，非原计划）

P0 复查结论：禁言不动 joined ZSET（live 路径正确），但缓存重建源
[circle_repo_pg.go:213 ListJoinedWithScore](../pkg/domains/circle/infrastructure/circle_repo_pg.go)
只查 `status = 1` → ZSET 过期/冷重建后，**禁言中的圈子会从"我的圈子"暂时消失**，与 live 路径矛盾。
修复：改为 `status IN (1, 2)`。

---

## 三、需求 1 实施设计：通知扇出

### 3.1 现有链路（不要新建第二套）

```
触发域 infra publisher ──► redpanda.PublishNotificationEvent(NotificationEventMessage)
      ──► topic notification_events ──► notification_consumer.go 聚合器(5s flush)
      ──► 批量反查接收人(post/comment) → R1 自动作过滤 → R4 去重
      ──► ON CONFLICT 批量 upsert domains.notification ──► INCRBY notice:unread:{uid}
```

范式参照：[like_event_publisher.go:44-49](../pkg/domains/like/infrastructure/like_event_publisher.go)（触发端写法）、
[notification_consumer.go:280-350](../pkg/server/storage/redpanda/notification_consumer.go)（接收人解析 + addRow）。
**沿用此链路，只做三处扩展：事件 schema、consumer 分支、读侧展示。**

### 3.2 事件 schema 扩展（[redpanda/constants.go:123](../pkg/server/storage/redpanda/constants.go)）

```go
type NotificationEventMessage struct {
    // ...现有字段不动...
    CircleID        *uuid.UUID `json:"circle_id,omitempty"`        // 新增：圈子类通知跳转/去重用（type 7-13 必填）
    RecipientUserID *uuid.UUID `json:"recipient_user_id,omitempty"` // 新增：圈子类通知接收人（触发端已知，type 7-13 必填）
}
```

- **不复用 MentionUserIDs**：语义混乱且批量语义（slice）与单接收人不符；`RecipientUserID` 与 mention 分支互不干扰。
- 新类型常量加到 `redpanda/constants.go`（与 NoticeType* 字符串常量并列）：
  `circle_muted / circle_banned / circle_admin_set / circle_admin_removed / circle_owner_transferred / circle_join_approved / circle_join_rejected`。

### 3.3 DDL：`domains.notification` 加列 + 去重索引重建

> ⚠️ 唯一去重索引 [notice.md uk_notice_dedup](notice.md) 现为
> `(recipient_id, actor_id, notice_type, COALESCE(post_id,0), COALESCE(comment_id,0))`。
> 场景：同一管理员管理 A/B 两个圈，把同一人分别拉黑 → 两条事件 (recipient, actor, type) 完全相同，
> **若索引不含 circle_id 会坍缩成一行**（后一条覆盖前一条）。circle_id 必须进去重键。

`docs/pgsql-ddl/notice.md` 权威 DDL 原地更新（新装环境直接生效），存量环境由 DB-owner 执行迁移：

```sql
-- 迁移脚本（存量环境，DB-owner 执行；授权见 pgsql-ddl/README.md 附录）
ALTER TABLE domains.notification ADD COLUMN circle_id UUID;
COMMENT ON COLUMN domains.notification.circle_id IS '圈子ID(UUID, 可空); 圈子管理类通知(7-13)必填, 跳转与去重锚点';

-- 重建去重索引：circle_id 纳入键（既有类型 circle_id 为 NULL → COALESCE 零值，去重行为不变）
DROP INDEX IF EXISTS domains.uk_notice_dedup;
CREATE UNIQUE INDEX uk_notice_dedup ON domains.notification(
    recipient_id, actor_id, notice_type,
    COALESCE(post_id,    '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(comment_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(circle_id,  '00000000-0000-0000-0000-000000000000'::uuid)
) WHERE deleted = 0;
```

同步改动：`notice/domain/notification.go` 实体加 `CircleID *uuid.UUID`；consumer 的 upsert 行结构补列。

### 3.4 consumer 扩展（[notification_consumer.go](../../pkg/server/storage/redpanda/notification_consumer.go)）

1. `noticeTypeCode` map（:23）加 7 个字符串→编码映射；编码常量加到
   [notice/domain/notification.go:17-24](../pkg/domains/notice/domain/notification.go)（`NoticeTypeCircleMuted int16 = 7` … `NoticeTypeCircleJoinRejected = 13`）。
2. 接收人解析 switch（:306）加一个分支，覆盖全部 7 个圈子类型：

```go
case NoticeTypeCircleMuted, NoticeTypeCircleBanned, NoticeTypeCircleAdminSet,
     NoticeTypeCircleAdminRemoved, NoticeTypeCircleOwnerTransferred,
     NoticeTypeCircleJoinApproved, NoticeTypeCircleJoinRejected:
    if event.CircleID == nil || event.RecipientUserID == nil {
        continue // 非法 schema，丢弃（log）
    }
    circle, ok := circles[*event.CircleID]
    if !ok {
        continue // 圈子已删（反查 miss，与 post/comment 目标删除同语义）
    }
    snippet := circle.Name // 默认快照=圈子名；mute 的时长文案由触发端预填 event.Snippet 覆盖（见 3.5）
    if event.Snippet != "" {
        snippet = event.Snippet
    }
    addRow(*event.RecipientUserID, event, snippet)
```

3. `collectLookupIDs`（:378）加圈子类型收集 `CircleID`；新增 `lookupCircles` 批量反查
   `domains.circle`（`SELECT id, name WHERE id IN ? AND deleted = 0`）——照抄 `lookupPosts` 风格，
   consumer 直读 DB 是既有约定（consumer 本就 import pgsql 直写）。
4. R1 自动作过滤（addRow 内 `recipient == event.ActorID`）**无需改动**：
   转让场景用 actor/recipient 互换（见下）天然绕开。

**转让的"通知双方"实现**：发两条事件，actor 互换，规避 R1（接收人==触发人被过滤）：

```go
// TransferOwner 成功后：
// ① 通知新圈主："你已成为《圈》圈主"（actor=旧圈主）
publish(type=circle_owner_transferred, actor=旧owner, recipient=新owner, circle)
// ② 通知旧圈主："圈主已转让给 X"（actor=新圈主，列表展示新圈主头像/名字，语义通顺）
publish(type=circle_owner_transferred, actor=新owner, recipient=旧owner, circle)
```

两条事件的去重键因 actor 不同而天然分开，无冲突。

### 3.5 触发端接入（circle 域）

**端口定义**（对齐 like 域范式——domain 声明小端口 + infra 薄适配器）：

- `circle/domain/repository.go`（或独立 `ports.go`）加：

```go
// NoticeEventPublisher 圈子管理通知事件发布端口（由 infrastructure 适配 Redpanda）。
type NoticeEventPublisher interface {
    PublishManagementNotice(ctx context.Context, event ManagementNoticeEvent) error
}
// ManagementNoticeEvent 圈子管理通知事件（触发端已知接收人）。
type ManagementNoticeEvent struct {
    Type     string // "circle_muted" 等 7 种
    ActorID  uuid.UUID // 操作者（转让第二条事件传新圈主，见 3.4）
    CircleID uuid.UUID
    TargetID uuid.UUID // 接收人
    Snippet  string    // 可选覆盖；mute 场景传 "被禁言 N 小时"
}
```

- `circle/infrastructure/` 新建 `notice_event_publisher.go` 薄适配器，委托
  `redpanda.PublishNotificationEvent`（照抄 [circle_event_publisher.go](../pkg/domains/circle/infrastructure/circle_event_publisher.go) 结构）。
- 注入：`NewCircleService` 加第 8 参（[composition/server.go:317-330](../pkg/composition/server.go) 补一行构造），
  或 setter 注入（对齐 userFacade/postFetcher 的可选依赖模式，**推荐后者**，composition 改动最小）。

**9 个发布点**（全部在 manage.go 对应方法**成功返回前**，失败仅 log）：

| 方法 | 事件 |
|---|---|
| MuteMember | muted，Snippet=`fmt.Sprintf("被禁言 %d 小时", durationHours)` |
| BanMember | banned |
| SetMemberRole | role==20 → admin_set；role==10 → admin_removed |
| TransferOwner | transferred ×2（actor 互换，3.4） |
| ReviewJoinRequest | approve → join_approved；否则 join_rejected |
| UnmuteMember / UnbanMember / UpdateCircleProfile / ListCircleMembers | 不发 |

### 3.6 读侧（notice 域）

- `NoticeItem`（[notice/application/service.go:52](../pkg/domains/notice/application/service.go)）加
  `CircleID string \`json:"circle_id,omitempty"\``，列表组装时透传。
- `ListNotifications` 的类型校验上界 `NoticeTypeMention` → `NoticeTypeCircleJoinRejected`（新常量）。
- 前端契约：更新 [notice-frontend-api.md](notice-frontend-api.md)——新类型 7-13 的文案模板、
  `circle_id` 字段、分类 tab 是否收录新类型（见确认项 #1）。

---

## 四、需求 2 实施设计：审计表

### 4.1 DDL（追加到 [circle.md](pgsql-ddl/circle.md)，DB-owner 执行）

```sql
-- 圈子管理操作审计表 (append-only: 只 INSERT, 不 UPDATE/DELETE)
CREATE TABLE domains.circle_member_audit (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    circle_id UUID NOT NULL,              -- 圈子ID
    operator_id UUID NOT NULL,            -- 操作者(圈主/管理员)
    target_id UUID,                       -- 目标成员(编辑圈子资料类操作为 NULL)
    action SMALLINT NOT NULL,             -- 1=设管理员 2=免管理员 3=转让圈主 4=禁言 5=解禁 6=拉黑 7=解黑 8=审核通过 9=审核拒绝 10=编辑圈子资料
    reason VARCHAR(200),                  -- 操作原因(可选, 前端传入)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
COMMENT ON TABLE domains.circle_member_audit IS '圈子管理操作审计表(append-only)';
CREATE INDEX idx_circle_audit_circle ON domains.circle_member_audit(circle_id, id DESC);
```

约定说明：无 `deleted` 列——审计日志不可变，逻辑删除无意义（与业务表约定的差异在此显式声明）；
无 FK/CHECK（全局约定）；action 枚举合法性由应用层保证。

### 4.2 代码改动

| 位置 | 改动 |
|---|---|
| `circle/domain/repository.go` | 实体 `CircleMemberAudit`（gorm tag + TableName=`domains.circle_member_audit`）+ action 常量（`AuditActionSetAdmin=1` … `AuditActionUpdateCircle=10`）+ 端口 `CircleAuditRepository { Create(ctx, *CircleMemberAudit) error }` |
| `circle/infrastructure/circle_audit_repo_pg.go` | 新建：单行 INSERT，显式 `sharedomain.NewID()`（实体未嵌 BaseModel，无 BeforeCreate 钩子） |
| `circle/application/manage.go` | `circleServiceImpl` 加 `auditRepo CircleAuditRepository` 依赖（setter 注入，nil 安全：未注入则跳过审计）+ 私有 helper `writeAudit(ctx, circleID, operatorID, targetID, action, reason)`；9 个管理方法成功后调用（UpdateCircleProfile 传 targetID=nil, action=10） |
| handler | 各 manage Request DTO 加可选 `Reason string \`json:"reason" binding:"omitempty,max=200"\``；service 输入结构透传；**过 `utils.SanitizeForPg`** |
| 测试 | action 枚举与 9 个方法→action 的映射关系建议用表驱动断言（如抽 `auditActionFor(op)` 纯函数则可直接单测） |

**写失败语义**：审计 INSERT 失败仅记日志、不回滚业务操作（管理操作已成功，审计是旁路；
P2 若要求零丢失，走 outbox/Redpanda，本期不做）。

---

## 五、需求 3 实施设计：普通成员可见成员列表

改动集中在 `manage.go ListCircleMembers`（:122）的权限分支，其余（repo/handler）零改动：

```
现逻辑：requireManageRole(admin) 不满足 → 403
改为：
  operator 加载后分支：
  ├─ role >= admin 且 status=normal → 现行为（全量视图，role/status 过滤自由）
  ├─ role == member(10) 且 status == normal → 受限视图：
  │     status 强制为 1（normal），请求里的 status 参数静默覆盖（不报 400，见确认项 #4）
  │     role 过滤保留（看管理员/圈主名单无敏感性）
  └─ 其他（非成员 / pending / muted / banned / left）→ 403 errNotCircleAdmin（现状不变）
```

实现建议：把 `requireManageRole` 拆出"仅加载+校验在圈正常成员"的轻量路径，或在
ListCircleMembers 内先 `GetMember` 自行分支（避免 requireManageRole 的 403 语义被复用污染）。
受限视图返回体结构不变（前端按角色渲染操作按钮，本身就会对非管理角色隐藏管理入口）。

---

## 六、需求 4 实施设计：joined ZSET 重建一致性

[circle_repo_pg.go:213 ListJoinedWithScore](../pkg/domains/circle/infrastructure/circle_repo_pg.go)
一处改动：

```go
// 修复前：WHERE user_id = ? AND status = 1
// 修复后：
Where("user_id = ? AND status IN ?", userID, []int16{domain.MemberStatusNormal, domain.MemberStatusMuted})
```

影响面：①"我的圈子"重建（`ensureJoinedWarm`）不再丢禁言中的圈子，与 live 路径（禁言不动 ZSET）一致；
② recommend C1 兴趣圈召回：被禁言用户的禁言圈仍参与推荐——合理（禁言只禁发言不禁浏览，见 post 域
发帖校验消费 mute_end_time 的既有语义）。改动一行 + 注释，随 P1.1 一起发。

---

## 七、数据流（P1.3 通知链路全景）

```
圈主/管理员操作                 circle/application/manage.go            circle/infra                 Redpanda            notification_consumer
──────────────               ───────────────────────────            ────────────────            ──────────           ─────────────────────
MuteMember ─┐
BanMember   │
SetMemberRole│  业务成功(CAS) ──► noticePublisher.PublishManagementNotice ──► PublishNotificationEvent ──► topic ──► flush(5s):
TransferOwner│  (事件发布失败      (端口, setter 注入)                        (Async, 失败仅 log)              1. 反查 domains.circle(name)
ReviewJoin ──┘   仅 log 不阻断)                                                                          2. R1 过滤 + R4 去重(circle_id 入键)
                                                                                                        3. ON CONFLICT upsert notification(+circle_id 列)
审计旁路(同步):  每个管理方法成功后 ──► CircleAuditRepository.Create ──► domains.circle_member_audit        4. INCRBY notice:unread:{uid}
                (失败仅 log; P2 可上 outbox)                                                          读侧: GET /notice/list(+circle_id, 类型 7-13)

成员列表: GET /circle/members ──► ListCircleMembers 权限分支 ──► admin+: 全量 / normal 成员: 强制 status=1 ──► UserFacade 回填
```

---

## 八、Schema / 配置变更汇总

| 变更 | 类型 | 执行者 |
|---|---|---|
| `domains.notification` 加 `circle_id` 列 + 重建 `uk_notice_dedup` | DDL 迁移（§3.3） | DB-owner |
| `docs/pgsql-ddl/notice.md` 权威 DDL 同步更新 | 文档 | 开发者 |
| 新表 `domains.circle_member_audit` + 索引 | DDL（§4.1） | DB-owner |
| `docs/pgsql-ddl/circle.md` 追加审计表 | 文档 | 开发者 |
| 无新配置项、无新 Redis key、无新 topic（复用 `notification_events`） | — | — |
| 前端契约：notice 类型 7-13 + `circle_id` 字段；manage 请求可选 `reason` | 文档（notice-frontend-api.md / api 目录） | 开发者 + 前端对齐 |

---

## 九、一致性 / 边界 / 风险

| 项 | 决策 |
|---|---|
| 通知发布失败 | 仅 log 不阻断管理操作（对齐全域 best-effort 哲学）；通知可从管理列表状态推断，可接受 |
| consumer 重复投递 | `uk_notice_dedup` upsert 幂等（circle_id 已入键）；INCRBY 多加靠读 miss 回源自愈 |
| 同人同操作重复触发（反复禁言） | upsert 复用行 + 重置未读（R3 既有语义，禁言→解禁→再禁言 = 一行） |
| 转让的 R1 冲突 | actor/recipient 互换两条事件（§3.4），不引入 R1 旁路开关 |
| 圈子被删后通知 | 反查 miss → 丢弃（对齐 post/comment 目标删除语义；存量行保留可读） |
| 审计丢失窗口 | 同步 INSERT 失败仅 log；管理操作低频，P2 上 outbox 若合规要求零丢失 |
| 审计表膨胀 | append-only + 管理操作低频，量级可控；P2 加保留期清理 job（同 notification P2 项） |
| 普通成员视角泄漏 | 受限视图强制 status=1，pending/禁言/拉黑名单不可见；mute_end_time 对 normal 成员恒为 NULL，无泄漏 |
| ZSET 修复的缓存残缺 | 已按旧规则重建的 ZSET 混有缺失（禁言中用户），24h TTL 自然收敛，无需主动清洗 |
| 通知类型枚举边界 | notice 读侧校验上界、consumer 未知类型丢弃（现状），两端常量必须同步加 |

---

## 十、分阶段交付建议

| 阶段 | 内容 | 规模 |
|---|---|---|
| **P1.1** | 需求 3（成员列表开放）+ 需求 4（ZSET 一行修复） | 小，纯 circle 域内改动，无 DDL 无前端，可独立先发 |
| **P1.2** | 需求 2（审计表 + 写审计 + reason 字段） | 中，依赖 DB-owner 建表；无前端强依赖（reason 可后补 UI） |
| **P1.3** | 需求 1（通知扇出） | 大，DDL 迁移 + consumer + 触发端 + 读侧 + 前端文案，需与前端排期对齐 |

依赖关系：三项互相独立，可并行/任意顺序；建议按上表从小到大交付。
每阶段验收：`go build ./... && go vet ./... && go test ./pkg/...` 全绿 +
§十一 对应场景手测。

---

## 十一、验收清单（手测场景）

**P1.1**
- [ ] 普通成员 GET /circle/members 只见 normal 成员；传 status=0/3 被静默覆盖仍只见 normal
- [ ] 非成员/待审/被拉黑用户 GET /circle/members → 403
- [ ] admin/owner 行为与 P0 完全一致（全量视图 + 过滤）
- [ ] 用户被禁言后：删其 joined ZSET → GET /circle/my 仍含该圈

**P1.2**
- [ ] 9 类管理操作各产生一行审计；编辑圈子资料 target_id 为 NULL、action=10
- [ ] reason 带控制字符/超长被 SanitizeForPg + 截断
- [ ] 审计 INSERT 人为注入失败（如回收权限）→ 管理操作仍成功、仅 error 日志

**P1.3**
- [ ] 禁言/拉黑/任免/审核通过/拒绝 → 目标用户 notice 列表出现对应类型，含 circle_id，未读 +1
- [ ] 转让后双方各收到一条（actor 互换）；同管理员在两个圈拉黑同一人 → 两条通知不坍缩
- [ ] 同 (recipient, actor, circle, type) 重复操作 → 仍一行、未读重置
- [ ] 禁言通知 snippet 显示"N 小时"；其他类型显示圈子名
- [ ] 既有 1-6 类通知回归不受影响（去重索引重建后）

---

## 十二、关键确认项（实施前与产品/前端逐条确认）

1. **通知类型粒度**：7 种细分（本文方案，前端逐类型配文案）vs 合并为 2-3 种（如"管理动作/审核结果"）。推荐 7 种。
2. **notice 分类 tab**：新类型 7-13 是否收录进前端 tab（还是仅"全部"可见）。推荐默认收录"系统/圈子"类 tab 或"全部"。
3. **转让双方通知**：actor 互换两条（本文方案）vs 仅通知新圈主（旧圈主自己操作的，感知弱）。前者忠实于原设计"通知双方"。
4. **普通成员传 status 过滤**：静默覆盖为 normal（本文方案，前端省事）vs 显式 400。
5. **非成员/访客能否看成员列表**：本文按原矩阵严格限定为"在圈正常成员+管理"（非成员 403）vs 放开给所有登录用户（常见社区做法）。推荐先按矩阵，产品有诉求再放开。
6. **审计是否含 reason 与"编辑圈子资料"**：本文含（action=10, reason 可选）；若产品只要成员操作审计，去掉 action=10 与 reason 字段即可。
7. **审计查询端点**（GET /circle/audit-log，owner 可看本圈审计）：本文未设计（原设计文档未提），建议 P2。

---

## 十三、开发红线提醒（违反即返工，全文见 .agents/skills/qubar-skill/SKILL.md）

- circle 域**不得 import** notice/redpanda 包：通知发布走 domain 端口 + infra 适配器（§3.5）。
- DDL 一律落 `docs/pgsql-ddl/`，由 DB-owner 执行；运行时角色无 ALTER 权限，**禁止 AutoMigrate**。
- 用户文本（reason）入库前必须 `utils.SanitizeForPg`，在 application 层调用。
- handler 只 bind + 调 service + 映射错误；响应用 `httputil` 助手，禁 `c.JSON`。
- 审计实体未嵌 BaseModel → INSERT 前显式 `sharedomain.NewID()`。
- 新增纯函数（类型映射/action 映射等）补表驱动单测；完成后 `go build ./... && go vet ./... && go test ./pkg/...`。

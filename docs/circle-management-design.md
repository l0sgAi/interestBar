# 圈子管理(circle management)设计文档

> 目标：圈主/管理员对成员做角色变更、禁言、拉黑/解禁、入圈审核；圈主/管理员编辑圈子资料。
> 基线：`circle_member` 表列已齐(role/status/mute_end_time/reputation),枚举已定义(`pkg/enums/circle.go`),
> 但服务层仅有 join/leave([service.go:477-538](../pkg/domains/circle/application/service.go))，无任何管理操作。

## 一、现状盘点

| 资产 | 位置 | 状态 |
|---|---|---|
| `circle_member` 表 | [docs/pgsql-ddl/circle.md:167](pgsql-ddl/circle.md) | role/status/mute_end_time 列已存在,**无需 DDL 变更** |
| 角色/状态枚举 | `pkg/enums/circle.go:22-39` | 10/20/30、0-4 齐；`IsAdmin()/IsOwner()` 已有 |
| 领域常量 | `pkg/domains/circle/domain/circle.go` | `MemberRoleOwner`/`MemberStatusNormal` 等已存在 |
| `MemberRepository` | [repository.go:32-43](../pkg/domains/circle/domain/repository.go) | 仅 GetMember/ListJoinedWithScore/JoinCircle/LeaveCircle |
| 发帖权限消费 | `post/application/service.go:60-71` `CircleMemberChecker` | 已消费 status + mute_end_time,禁言天然生效 |
| 成员计数链路 | JoinCircle/LeaveCircle:Redis Incr/Decr + PublishMemberCount + joinedCache ZSET | 拉黑/踢出必须复用同一链路 |
| 圈子资料缓存 | `CircleBaseCache`(base info) | 编辑圈子后必须失效 |
| 圈子 ES | `CircleSearcher`(ES);DB 变更走外部 CDC | 编辑资料改 DB 即可,ES 由 CDC 追 |

## 二、需求分析

### 操作者

- **圈主(owner, role=30)**:每圈唯一(建圈事务写入,[circle_repo_pg.go:84-108](../pkg/domains/circle/infrastructure/circle_repo_pg.go))。
- **管理员(admin, role=20)**:由圈主任命。

### 操作集(对成员)

| 操作 | 目标状态迁移 | 附加字段 | 副作用 |
|---|---|---|---|
| 设为管理员 | role 10→20 | — | 通知 |
| 取消管理员 | role 20→10 | — | 通知 |
| 转让圈主 | 30→10 且 10→30(同事务) | — | 通知双方 |
| 禁言 | status→2 | mute_end_time=N 小时后 | 通知 |
| 解除禁言 | 2→1 | mute_end_time=NULL | — |
| 拉黑/踢出 | →3 | — | **若原 status=1:member_count-1 + joinedCache 移除** |
| 解除拉黑 | 3→4(left) | — | 用户需重新申请加入(JoinCircle left→normal 已有路径) |
| 审核通过 | 0→1 | — | member_count+1 + joinedCache 写入 + 通知 |
| 审核拒绝 | 0→4(left) | — | 通知 |

### 操作集(对圈子资料)

`name / slug / avatar_url / cover_url / description / rule / category_id / join_type` 全部可编辑。

### 权限矩阵

| 操作 | owner | admin | 普通成员 |
|---|---|---|---|
| 转让圈主 / 任免管理员 | ✅ | ❌ | ❌ |
| 禁言/拉黑/审核 admin | ✅ | ❌ | ❌ |
| 禁言/拉黑/审核 普通成员 | ✅ | ✅ | ❌ |
| 编辑 name/slug/join_type/category | ✅ | ❌ | ❌ |
| 编辑 avatar/cover/description/rule | ✅ | ✅ | ❌ |
| 查看成员列表(含 pending/拉黑) | ✅ | ✅ | ❌(仅见 normal 成员,P1) |

核心规则:**只能管理角色严格低于自己的人**(owner>admin>member);owner 不可被禁言/拉黑/降级(除转让)。

### 边界情形

1. **禁言过期**:status=2 且 mute_end_time 已过 → 读路径(GetMember)惰性自愈回 status=1。发帖校验已消费 mute_end_time,过期自然放行;自愈保证管理列表展示正确。
2. **拉黑 ≠ 退出**:拉黑保留记录防重进(JoinCircle 对 banned 已拒绝);解除拉黑落 status=4(left)而非 1,避免"偷偷回圈"。
3. **owner 退圈**:LeaveCircle 已拒绝([circle_repo_pg.go:222-224](../pkg/domains/circle/infrastructure/circle_repo_pg.go))。owner 想退出只能先转让——本期不做"解散圈子"。
4. **计数一致性**:仅 status=1↔其他 的迁移触发 member_count 增减;0(pending)不算成员,2(禁言)仍是成员(不减),3(拉黑)减,4(left)减(已有 LeaveCircle 链路)。
5. **name/slug 唯一**:沿用 `ExistsByName/ExistsBySlug`,冲突返回 409。

## 三、API 设计(全部挂 authCheck,权限校验在 service 层)

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/circle/members?circle_id=&role=&status=&cursor=&size=` | admin+ | 成员列表,keyset 分页(对齐 `idx_member_circle_role`:role DESC, create_time DESC, id) |
| POST | `/circle/manage/role` | owner | body: circle_id, target_user_id, role(10|20);role=30 走转让 |
| POST | `/circle/manage/transfer` | owner | body: circle_id, target_user_id |
| POST | `/circle/manage/mute` | admin+ | body: circle_id, target_user_id, duration_hours |
| POST | `/circle/manage/unmute` | admin+ | body: circle_id, target_user_id |
| POST | `/circle/manage/ban` | admin+ | body: circle_id, target_user_id |
| POST | `/circle/manage/unban` | admin+ | body: circle_id, target_user_id |
| POST | `/circle/manage/review` | admin+ | body: circle_id, target_user_id, approve(bool) |
| PUT | `/circle/update` | owner/admin 分字段 | body: circle_id + 可选字段,name/slug/join_type/category_id 仅 owner |

## 四、数据流

```
handler(bind+requireUserID)
  → service:
      1. GetMember(circleID, operatorID)  → 取操作者 role
      2. GetMember(circleID, targetID)    → 取目标(校验存在/角色低于操作者)
      3. 权限矩阵校验 → errForbidden / errCannotManage
      4. memberRepo.UpdateXxx(状态迁移,owner 转让用事务)
      5. 副作用:
         - status 跨 1 边界 → statsCache Incr/DecrMemberCount + PublishMemberCount(±1)
         - 拉黑 → joinedCache.Remove;审核通过 → joinedCache.Add
         - 编辑圈子 → repo.Update + baseCache 失效(Delete),ES 由 CDC 追
         - (P1) notice 域扇出通知
```

## 五、Schema/配置变更

- **P0 无 DDL 变更**(列全齐)。
- P1 可选:管理操作审计表 `circle_member_audit`(id, circle_id, operator_id, target_id, action, reason, create_time),追加到 [docs/pgsql-ddl/circle.md](pgsql-ddl/circle.md),由 DB-owner 执行。
- 无新配置项、无新 Redis key(复用现有缓存接口)。

## 六、一致性/边界/风险

| 项 | 决策 |
|---|---|
| 禁言过期 | GetMember 惰性自愈(读时过期→update status=1 返回 normal),不建定时 job |
| 并发操作同一成员 | 状态机校验在 update 时带 `WHERE status=旧值` 条件,0 行受影响 → 409 冲突 |
| 转让圈主 | 单事务两条 update;目标必须是 normal 成员 |
| member_count 漂移 | 复用现有 write-behind 链路(Redis 真值+事件落库),与 join/leave 一致 |
| 编辑圈子 name 冲突 | ExistsByName 预检 + DB 唯一索引兜底 → 409 |
| ES 延迟 | 资料编辑后 ES 靠 CDC,秒级延迟,详情页读 DB/缓存不受影响 |

## 七、分阶段交付

| 阶段 | 内容 |
|---|---|
| **P0** | repo 新方法(ListMembers/UpdateRole/UpdateStatus/UpdateCircle/TransferOwner);service 权限矩阵+8 个管理方法+编辑圈子;handler+routes;GetMember 惰性解禁;计数/缓存副作用;`errors.go` 新错误谓词 |
| **P1** | notice 域通知扇出(被禁言/拉黑/任免/审核结果);审计表+写审计;普通成员可见的成员列表(normal only) |

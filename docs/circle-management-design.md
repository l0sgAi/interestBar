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

## 八、P0.1 追加:成员搜索(已实施)

> 需求:管理端成员列表支持按**用户名/邮箱**搜索(超大圈友好);P0 的 ListMembers 只有 role/status 过滤。

### 8.1 方案:复用 user 域 ES 搜索,Facade 桥接(不建第二套)

ES 用户索引**已同时检索 username(权重3)/email(权重1)**(`elasticsearch/user.go:87` multi_match,
@提及 用户搜索同一链路),email 不落任何响应字段(仅用于匹配,无隐私泄漏)。跨域走 Facade 红线不变。

```
GET /circle/members?keyword=x
  → circle/application/manage.go ListCircleMembers
      1. 权限校验(admin+,关键词搜索在权限之后,防未授权消耗 ES)
      2. userFacade.SearchBriefs(keyword, 100)          ← 新增 Facade 方法
         composition 桥接 → user.application.UserFacade.SearchBriefs
           → userFacadeAdapter: ES SearchUsers(multi_match, 按 _score 排序) → []UserBrief
      3. 命中 0 个 → 直接空页;命中 ≤100 个 → user_id IN (...) 过滤成员表
      4. memberRepo.ListMembers(+userIDs 过滤, 与 role/status/游标条件正交组合)
```

### 8.2 大圈正确性论证(为什么游标翻页仍精确)

- `circle_member` 对 (circle_id, user_id) 唯一 → 关键词命中 N 个用户 ⇒ 过滤后**至多 N 行**;
- N ≤ memberSearchMaxUsers=100(`manage.go`) → 结果集有限,`(role, create_time, id)` keyset 游标
  与 `user_id IN` 条件正交组合,翻页不重不漏;
- 超过 100 命中取相关性 Top-100(ES 按 _score DESC),前端提示细化关键词;
- 翻页须带同一 keyword(keyword 变化时客户端重置 cursor)——同一关键词下候选集视为页间一致快照。

### 8.3 改动清单

| 位置 | 改动 |
|---|---|
| `user/application/service.go` | UserFacade 接口 + userFacadeAdapter 加 `SearchBriefs(keyword, limit)`(空关键词短路,limit ≤100 对齐 ES size 语义,直接映射 ES 结果不回源) |
| `circle/application/service.go` | circle 域 UserFacade 接口 + CircleService.ListCircleMembers 签名加 keyword |
| `circle/domain/repository.go` | ListMembers 加 `userIDs []uuid.UUID` 参数(nil=不过滤) |
| `circle/infrastructure/circle_repo_pg.go` | `user_id IN ?` 过滤(走 idx_member_unique 前缀) |
| `circle/application/manage.go` | 搜索分支 + `memberSearchMaxUsers=100`;ES 失败 → `errUserSearchUnavailable`(503) |
| `circle/application/errors.go` | errUserSearchUnavailable + IsUserSearchUnavailableErr |
| `composition/facade_bridges.go` | circleUserFacade.SearchBriefs 保序类型映射 |
| `circle/interfaces/http/handler.go` | keyword query 参数 + 503 映射 |
| 无 DDL/无新 Redis key/无新 topic | ES 索引与 CDC 链路现成 |

### 8.4 边界/风险

| 项 | 决策 |
|---|---|
| ES 不可用 | 成员搜索返回 503(不静默降级为"无结果",避免管理员误判);不带 keyword 的列表不受影响 |
| ES 索引延迟(CDC) | 新注册用户秒级内可能搜不到,与 @提及 用户搜索一致,接受 |
| email 匹配 | ES 分词匹配(非精确等于);email 永不进入响应体 |
| keyword 为空串 | 短路,行为与 P0 完全一致(不触发 ES 查询) |
| 无 keyword 时 userIDs=nil | ListMembers 走原路径,零额外开销 |

## 九、P0.2 追加:搜索拼写容错 + 截断显式化(已实施)

> 需求:成员搜索支持**拼写容错**(如 "alic" 命中 "alice");命中超 100 截断从静默改为显式返回。

### 9.1 方案:ES 查询增强 + total 透传(不加第二套链路)

ES `SearchUsers` 关键词分支从单 `multi_match` 改为 `bool should` 三路召回
(`minimum_should_match=1`,`elasticsearch/user.go` buildUserSearchQuery):

1. `username multi_match + fuzziness:"AUTO"`(权重 3)——拼写容错,编辑距离自适应
   (1-2 字符词 0、3-5 字符词 1、更长 2);
2. `username.keyword wildcard` 子串包含——真·模糊匹配("mie" 中 "miemie"、"君几" 中
   "盼君几多愁"),`case_insensitive:true` 大小写不敏感;用户输入 `* ? \` 经
   `escapeWildcard` 转义防通配注入(全词表扫描慢查询);
3. `email match`(不做容错、不做子串)——仅整串分词匹配,隐私面不扩大。

> 教训:fuzziness(拼写容错)≠ 子串模糊。中文场景 fuzziness 近乎无效
> (≤2 字符词编辑距离为 0),子串召回只能靠 wildcard/ngram。初版只加了 fuzziness,
> 实测 "mie"→"miemie" 零召回后补 wildcard 一路。

**公开用户搜索与成员搜索共用此函数,模糊化同步生效**(用户域 `Search` API 行为随之改变)。
排序 `_score DESC, id DESC` 不变,search_after 语义不受影响(无关键词分支完全不动)。

截断显式化:ES 已 `track_total_hits`,`SearchBriefs` 签名改返 `([]UserBrief, total, error)`
三层透传(user 域 adapter → composition 桥 → circle 域);`total > 100` 即
`CircleMemberListResult.Truncated=true`,前端提示细化关键词。

### 9.2 为何候选集固定上限 100 而非直接分页

两阶段排序不同源:阶段 1 ES 按 `_score` 相关性,阶段 2 成员表按 `(role, create_time)`
keyset。要正确翻页必须先拿到**完整候选用户集**再交集排序;若 ES 也分页,第 N 页用户的
加入时间与已返回成员交错,页序即错。且 `IN` 列表长度与 ES 单次召回必须设界。
代价=召回超 100 截断,由 `truncated` 标志显式暴露。

### 9.3 改动清单(P0.2 增量)

| 位置 | 改动 |
|---|---|
| `server/storage/elasticsearch/user.go` | 关键词分支改 bool should 三路(username fuzziness AUTO + username.keyword wildcard 子串 + email match);查询构建抽为 `buildUserSearchQuery`;`escapeWildcard` 防通配注入 |
| `server/storage/elasticsearch/fuzzy_query.go` | 新增共享 helper `fuzzyShouldClauses`(分词容错 + 主字段 .keyword wildcard 子串);`escapeWildcard` 收敛于此 |
| `server/storage/elasticsearch/circle.go` | SearchCircles / SearchMyCircles 关键词分支改 `fuzzyShouldClauses(keyword,"name","description")`,精确命中 boost(should match_phrase/name.keyword)保留 |
| `server/storage/elasticsearch/post.go` | SearchPosts / SearchPostsByIDsAndKeyword / searchUserPostsInternal 关键词分支改 `fuzzyShouldClauses(keyword,"title","summary")` |
| `user/application/service.go` | `SearchBriefs` 签名加 `total int64` 返回值 |
| `circle/application/service.go` | circle 域 UserFacade.SearchBriefs 签名同步 |
| `composition/facade_bridges.go` | circleUserFacade.SearchBriefs 透传 total |
| `circle/application/manage.go` | `CircleMemberListResult` 加 `Truncated bool`;`total > memberSearchMaxUsers` 填充 |
| `docs/circle-management-frontend-api.md` | 搜索语义 + 响应体加 `truncated` |
| 无 DDL/无索引变更/无新配置 | 纯查询 DSL 与签名变更 |

### 9.4 边界/风险(P0.2 增量)

| 项 | 决策 |
|---|---|
| fuzziness 对中文用户名 | ik 分词后按词容错,单字词(≤2 字符)不容错;中文拼写容错收益有限但无害 |
| email 不做容错 | 防邮箱近似串误召回(隐私面不扩大) |
| 公开用户搜索行为变化 | 与成员搜索共用函数,同步获得容错;排序/分页契约不变 |
| 截断判定 | `total > 100` 用 ES track_total_hits 精确值,非"拿满 100 即猜" |

# 数据库表结构定义(UUIDv7 主键版)

> 中间件:PostgreSQL 18,内置 `uuidv7()` 函数(无需 CREATE EXTENSION,`uuid` 类型为 PG 内置)。
> 全表主键与外键统一使用 **UUIDv7**(`UUID` 类型)。UUIDv7 前 48 位为毫秒级时间戳,**字典序 == 时间序**,可直接用于「最新优先」排序与 keyset 游标分页。
>
> **主键生成策略**:
>
> - DDL 中 `DEFAULT uuidv7()` 仅为**手工 SQL 插入**时的兜底;
> - 应用层(GORM)统一在 `BeforeCreate` 钩子中生成 UUIDv7,避免 GORM 将零值(nil UUID)当作有效值覆盖默认值,并保证 `Create` 后 ID 立即可用。
>
> **可空外键(原 int64 用 `0` 作哨兵的字段)**改为 NULLABLE,语义为 `NULL = 无父级/无值`:
> `comment.root_id`、`comment.reply_to_id`、`comment.reply_to_user_id`、`category.parent_id`、`circle.category_id`、`comment_like.post_id`。

## 目录(按领域拆分)

| 文档 | 领域 | 表 |
|---|---|---|
| [user.md](user.md) | 用户 | `users` |
| [circle.md](circle.md) | 圈子 | `category`、`circle`、`circle_member` |
| [post.md](post.md) | 帖子 | `post` |
| [comment.md](comment.md) | 评论 | `comment`、`comment_like` |
| [interaction.md](interaction.md) | 帖子互动/流水 | `post_like`、`post_collect`、`post_view_history`、`post_interaction` |
| [ai-agent.md](ai-agent.md) | AI 回复机器人 | `ai_agent`、`ai_agent_reply_log` |
| [notice.md](notice.md) | 消息中心 | `notification` |

> 约定:无 FK/CHECK 约束(引用有效性、状态枚举合法性由应用层保证);
> 逻辑删除 `deleted SMALLINT`(0=正常,1=已删除);状态列 `status SMALLINT`,枚举含义见各表注释。

---

## 附录:应用角色权限(DB-owner 建表后必做)

运行时应用角色(`qubar_web_app`,见 `configs/config.yaml` 的 `pgsql.username`)对 `domains` schema 下的表**只有被显式 GRANT 的权限**。DB-owner 角色新建表后不授权,应用即报 `permission denied for table xxx (SQLSTATE 42501)`。

**建表后标准步骤**(以 DB-owner 身份执行):

```sql
-- 1. 新表现表授权(立即生效;一次授予 schema 下所有表现表)
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA domains TO qubar_web_app;

-- 2. 默认权限:以后本角色新建的表自动授权(新表免再手工 GRANT)
ALTER DEFAULT PRIVILEGES IN SCHEMA domains
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO qubar_web_app;
```

**约定**:

- 应用角色只给行级 DML(`SELECT/INSERT/UPDATE/DELETE`),**不给** `TRUNCATE/ALTER`——schema 变更一律走 DB-owner(与"运行时角色无 ALTER 权限、禁止 AutoMigrate"一致);
- UUID 主键无 sequence,无需 `GRANT USAGE ON SEQUENCE`;
- append-only 表(如 `ai_agent_reply_log`)可只授 `SELECT, INSERT`,不给 `UPDATE/DELETE`;
- `ALTER DEFAULT PRIVILEGES` 只对**执行该语句的角色**之后新建的表生效;若 DDL 由多个 owner 角色执行,需各自执行一次;
- 验证:`\dp domains.<table>` 或应用重试对应接口。

---

## 附录:批量更新统计的 jsonb 类型对照

异步消费者(MQ 聚合后批量写库)使用 `jsonb_to_recordset`,ID 列的类型必须从 `BIGINT` 改为 `uuid`:

```sql
-- 圈子成员/帖子计数 (circle_id)
UPDATE circle c
SET member_count = GREATEST(c.member_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(circle_id uuid, delta BIGINT)) v
WHERE c.id = v.circle_id AND c.deleted = 0;

-- 评论点赞数 (comment_id)
UPDATE comment c
SET like_count = GREATEST(c.like_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(comment_id uuid, delta BIGINT)) v
WHERE c.id = v.comment_id AND c.deleted = 0;

-- 帖子点赞数 (post_id)
UPDATE post p
SET like_count = GREATEST(p.like_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(post_id uuid, delta BIGINT)) v
WHERE p.id = v.post_id AND p.deleted = 0;

-- 帖子全量统计 (post_id + 多 delta)
UPDATE post p
SET view_count    = LEAST(GREATEST(p.view_count    + v.view_delta, 0), 1000000000),
    comment_count = GREATEST(p.comment_count + v.comment_delta, 0),
    like_count    = GREATEST(p.like_count    + v.like_delta, 0),
    collect_count = GREATEST(p.collect_count + v.collect_delta, 0),
    update_time   = CURRENT_TIMESTAMP
FROM (SELECT * FROM jsonb_to_recordset($1::jsonb)
      AS v(post_id uuid, view_delta BIGINT, comment_delta BIGINT, like_delta BIGINT, collect_delta BIGINT)) v
WHERE p.id = v.post_id AND p.deleted = 0;
```

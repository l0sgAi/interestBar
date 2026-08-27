# 帖子域(post)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

## 帖子表

```sql
DROP TABLE IF EXISTS domains.post;

CREATE TABLE domains.post (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 归属关系 (核心外键)
    circle_id UUID NOT NULL,          -- 所属圈子ID
    user_id UUID NOT NULL,            -- 发帖人ID

    -- 2. 内容主体
    type SMALLINT NOT NULL DEFAULT 1,   -- 帖子类型：1=图文, 2=纯视频, 3=投票/链接
    title VARCHAR(200) NOT NULL DEFAULT '', -- 标题 (允许为空，适配微博/朋友圈式的短内容)
    summary VARCHAR(2048) NOT NULL DEFAULT '', -- 摘要 (纯文本，用于推送/列表预览，去除了HTML标签)
    content TEXT NOT NULL DEFAULT '',   -- 正文 (富文本/Mardown/HTML)

    -- 3. 多媒体与扩展
    -- 存储图片URL列表、视频封面/地址、链接卡片信息等
    -- 结构示例: ["url1", "url2", "url3"]
    media_extra JSONB NOT NULL DEFAULT '[]'::JSONB,

    -- 4. 统计数据
    view_count INT NOT NULL DEFAULT 0,    -- 浏览量
    comment_count INT NOT NULL DEFAULT 0, -- 评论数
    like_count INT NOT NULL DEFAULT 0,    -- 点赞数
    collect_count INT NOT NULL DEFAULT 0, -- 收藏数
    hot INT NOT NULL DEFAULT 0,   -- 热度分

    -- 5. 运营与状态标记
    is_pinned SMALLINT NOT NULL DEFAULT 0,  -- 是否置顶：0=否, 1=是 (圈内置顶)
    is_essence SMALLINT NOT NULL DEFAULT 0, -- 是否加精：0=否, 1=是
    is_lock SMALLINT NOT NULL DEFAULT 0,    -- 是否锁定：0=否, 1=是 (禁止评论)

    -- 6. 审核与生命周期
    -- 状态：0=草稿, 1=发布(正常), 2=审核中(若开启先审后发), 3=审核失败, 4=被屏蔽(软删/违规)
    status SMALLINT NOT NULL DEFAULT 1,
    deleted SMALLINT DEFAULT 0, -- 用户自行删除：0=未删, 1=已删

    -- 7. 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_reply_time TIMESTAMPTZ -- 最后回复时间
);

-- --- 注释 ---
COMMENT ON TABLE domains.post IS '帖子主表';
COMMENT ON COLUMN domains.post.id IS '主键ID(UUIDv7)';
COMMENT ON COLUMN domains.post.circle_id IS '所属圈子ID(UUID)';
COMMENT ON COLUMN domains.post.user_id IS '作者用户ID(UUID)';
COMMENT ON COLUMN domains.post.type IS '类型:1=图文,2=视频,3=投票';
COMMENT ON COLUMN domains.post.title IS '标题';
COMMENT ON COLUMN domains.post.summary IS '纯文本摘要';
COMMENT ON COLUMN domains.post.content IS '正文内容(Text)';
COMMENT ON COLUMN domains.post.media_extra IS '媒体扩展信息(JSONB存储图片/视频)';
COMMENT ON COLUMN domains.post.view_count IS '浏览数';
COMMENT ON COLUMN domains.post.is_pinned IS '是否置顶';
COMMENT ON COLUMN domains.post.is_essence IS '是否加精';
COMMENT ON COLUMN domains.post.status IS '状态:1=正常,2=审核中,3=驳回,4=屏蔽';
COMMENT ON COLUMN domains.post.update_time IS '更新时间(ES同步锚点)';

-- --- 索引优化 ---

-- 1. 【核心】外键关联索引
-- 场景：用户进入圈子详情页，PG 校验圈子是否存在
CREATE INDEX idx_post_circle ON domains.post(circle_id);

-- 2. 【用户】"我的帖子" 列表
-- 场景：用户中心查看发布历史。这个通常直接走数据库，因为不仅要看正常的，还要看草稿/审核中的
CREATE INDEX idx_post_user ON domains.post(user_id, status, create_time DESC);

-- 3. 【管理】后台审核/管理索引
-- 场景：后台管理系统按状态筛选帖子进行人工审核
CREATE INDEX idx_post_status ON domains.post(status, create_time DESC);

-- 4. 【置顶】获取圈子置顶帖
-- 场景：置顶帖数量很少(3-5条)，通常不走ES，直接查DB置顶然后插在列表最前面
CREATE INDEX idx_post_pinned ON domains.post(circle_id) WHERE is_pinned = 1 AND deleted = 0;
```

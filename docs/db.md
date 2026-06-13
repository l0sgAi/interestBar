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

---

## 用户表

```sql
DROP TABLE IF EXISTS domains.users;

CREATE TABLE domains.users (
    id              UUID PRIMARY KEY DEFAULT uuidv7(),
    username        VARCHAR(50) NOT NULL,
    email           VARCHAR(100) NOT NULL,
    phone           VARCHAR(20),
    pwd             VARCHAR(512),
    motto           VARCHAR(2048),
    google_id       VARCHAR(100),
    x_id            VARCHAR(100),
    github_id       VARCHAR(100),
    microsoft_id    VARCHAR(100),
    avatar_url      VARCHAR(500),
    gender          SMALLINT DEFAULT 0,
    birthdate       DATE,
    status          SMALLINT DEFAULT 1,
    role            SMALLINT DEFAULT 0,
    deleted         SMALLINT DEFAULT 0,
    create_time     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 添加列注释
COMMENT ON COLUMN domains.users.id IS '主键ID(UUIDv7,应用层生成,DB默认值仅兜底)';
COMMENT ON COLUMN domains.users.username IS '用户名/昵称';
COMMENT ON COLUMN domains.users.email IS '邮箱(唯一凭证)';
COMMENT ON COLUMN domains.users.google_id IS 'Google平台唯一ID';
COMMENT ON COLUMN domains.users.x_id IS 'X/Twitter平台唯一ID';
COMMENT ON COLUMN domains.users.github_id IS 'Github平台唯一ID';
COMMENT ON COLUMN domains.users.status IS '状态：0=禁用，1=启用';
COMMENT ON COLUMN domains.users.deleted IS '逻辑删除：0=正常，1=已删除';

-- 索引
CREATE UNIQUE INDEX idx_users_email ON domains.users(email);
CREATE INDEX idx_users_google_id ON domains.users(google_id);
CREATE INDEX idx_users_x_id ON domains.users(x_id);
CREATE INDEX idx_users_github_id ON domains.users(github_id);
CREATE INDEX idx_users_phone ON domains.users(phone);
```

## 分类表

> **种子数据 ID 必须固化**(下方 INSERT 显式指定固定 UUIDv7),不可每次随机——否则重建库后 ID 变化,导致前端/配置缓存错乱。

```sql
DROP TABLE IF EXISTS domains.category;

CREATE TABLE domains.category (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心信息
    name VARCHAR(50) NOT NULL, -- 分类名称 (如：科技、生活)
    slug VARCHAR(60), -- SEO友好标识 (如：tech, life)
    icon VARCHAR(500), -- 图标/Icon URL

    -- 层级结构 (支持无限级或二级分类)
    parent_id UUID, -- 父分类ID，NULL表示顶级分类

    -- 排序与统计
    sort INT NOT NULL DEFAULT 0, -- 排序权重 (数值越大越靠前，或越小越靠前，由业务定义)
    circle_count INT NOT NULL DEFAULT 0, -- 该分类下的圈子数量 (缓存字段，用于展示 "科技(102)")

    -- 状态
    status SMALLINT NOT NULL DEFAULT 1, -- 0=禁用/隐藏，1=启用/显示
    deleted SMALLINT DEFAULT 0, -- 逻辑删除

    -- 时间 (用于同步)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 添加注释 ---
COMMENT ON TABLE domains.category IS '圈子分类表';
COMMENT ON COLUMN domains.category.id IS '分类ID(UUIDv7)';
COMMENT ON COLUMN domains.category.name IS '分类名称';
COMMENT ON COLUMN domains.category.slug IS 'URL标识符';
COMMENT ON COLUMN domains.category.icon IS '分类图标';
COMMENT ON COLUMN domains.category.parent_id IS '父分类ID(NULL为顶级)';
COMMENT ON COLUMN domains.category.sort IS '排序权重(人工控制顺序)';
COMMENT ON COLUMN domains.category.circle_count IS '该分类下圈子数(统计缓存)';
COMMENT ON COLUMN domains.category.status IS '状态:0=隐藏,1=显示';
COMMENT ON COLUMN domains.category.deleted IS '逻辑删除';
COMMENT ON COLUMN domains.category.update_time IS '更新时间(用于ES同步)';

-- --- 索引优化 (Index) ---

-- 1. 【必须】唯一性约束
CREATE UNIQUE INDEX idx_category_name ON domains.category(name) WHERE deleted = 0;
CREATE UNIQUE INDEX idx_category_slug ON domains.category(slug) WHERE deleted = 0;

-- 2. 【核心】列表查询索引
-- 场景：APP首页获取全部分类列表，通常按权重排序
-- SQL: SELECT * FROM category WHERE parent_id IS NULL AND status=1 AND deleted=0 ORDER BY sort DESC
CREATE INDEX idx_category_list ON domains.category(parent_id, sort DESC) WHERE deleted = 0 AND status = 1;

-- 插入顶级分类数据 (id 使用固化 UUIDv7，同一毫秒前缀 + 递增尾数)
INSERT INTO domains.category (id, name, slug, icon, parent_id, sort, circle_count) VALUES
-- 第一梯队：超高热度/刚需
('019058b0-5b40-7000-8000-000000000001', '科技数码', 'tech', NULL, NULL, 100, 0),
('019058b0-5b40-7000-8000-000000000002', '游戏电竞', 'gaming', NULL, NULL, 95, 0),
('019058b0-5b40-7000-8000-000000000003', '生活日常', 'lifestyle', NULL, NULL, 90, 0),
('019058b0-5b40-7000-8000-000000000004', '二次元', 'acg', NULL, NULL, 85, 0),
('019058b0-5b40-7000-8000-000000000005', '影音娱乐', 'entertainment', NULL, NULL, 80, 0), -- 电影、电视剧、综艺、音乐
('019058b0-5b40-7000-8000-000000000006', '流行文化', 'pop-culture', NULL, NULL, 79, 0),

('019058b0-5b40-7000-8000-000000000007', '运动健身', 'sports', NULL, NULL, 75, 0),
('019058b0-5b40-7000-8000-000000000008', '美食寻味', 'food', NULL, NULL, 70, 0),
('019058b0-5b40-7000-8000-000000000009', '萌宠动物', 'pets', NULL, NULL, 65, 0),
('019058b0-5b40-7000-8000-000000000010', '旅行户外', 'travel', NULL, NULL, 60, 0), -- 露营、旅游
('019058b0-5b40-7000-8000-000000000011', '汽车出行', 'cars', NULL, NULL, 55, 0), -- 汽车、摩托车

('019058b0-5b40-7000-8000-000000000012', '财经投资', 'finance', NULL, NULL, 50, 0), -- 股票、基金、理财
('019058b0-5b40-7000-8000-000000000013', '职场发展', 'career', NULL, NULL, 45, 0),
('019058b0-5b40-7000-8000-000000000014', '知识科普', 'knowledge', NULL, NULL, 40, 0), -- 硬核知识、百科
('019058b0-5b40-7000-8000-000000000015', '校园教育', 'education', NULL, NULL, 38, 0), -- 升学、考研、校园生活

('019058b0-5b40-7000-8000-000000000016', '阅读文学', 'reading', NULL, NULL, 35, 0), -- 书籍、网文
('019058b0-5b40-7000-8000-000000000017', '摄影摄像', 'photography', NULL, NULL, 30, 0),
('019058b0-5b40-7000-8000-000000000018', '时尚美妆', 'fashion', NULL, NULL, 25, 0),
('019058b0-5b40-7000-8000-000000000019', '艺术设计', 'art', NULL, NULL, 20, 0), -- 绘画、设计
('019058b0-5b40-7000-8000-000000000020', '家居房产', 'home', NULL, NULL, 15, 0), -- 装修、租房

('019058b0-5b40-7000-8000-000000000021', '亲子育儿', 'parenting', NULL, NULL, 10, 0),
('019058b0-5b40-7000-8000-000000000022', '情感心理', 'emotion', NULL, NULL, 5, 0),
('019058b0-5b40-7000-8000-000000000023', '新闻政治', 'history-politic', NULL, NULL, 4, 0),
('019058b0-5b40-7000-8000-000000000024', '历史人文', 'history-humannity', NULL, NULL, 3, 0),
('019058b0-5b40-7000-8000-000000000025', '其它兴趣', 'others', NULL, NULL, 2, 0),
('019058b0-5b40-7000-8000-000000000026', '小众猎奇', 'niche-exotic', NULL, NULL, 1, 0);
```

## 兴趣圈表

```sql
DROP TABLE IF EXISTS domains.circle;

CREATE TABLE domains.circle (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    name VARCHAR(50) NOT NULL, -- 兴趣圈名称
    slug VARCHAR(60), -- 唯一标识符(用于URL SEO，如 /circle/coding-life)，允许为空
    avatar_url VARCHAR(500), -- 兴趣圈头像url
    cover_url VARCHAR(500), -- 背景图url
    description TEXT NOT NULL, -- 描述信息
    rule TEXT NOT NULL, -- 圈内规则

    -- 3. 归属与分类
    creator_id UUID NOT NULL, -- 创建人ID (关联用户表)
    category_id UUID, -- 分类ID (关联分类表)，NULL表示未分类

    -- 4. 统计数据 (反范式设计，避免频繁 Count 查询)
    hot INT NOT NULL DEFAULT 0, -- 热度值 (计算所得)
    member_count INT NOT NULL DEFAULT 0, -- 成员数量
    post_count INT NOT NULL DEFAULT 0, -- 帖子数量

    -- 5. 状态与权限
    join_type SMALLINT NOT NULL DEFAULT 0, -- 加入方式：0=直接加入，1=需审核，2=私密(邀请制)
    status SMALLINT NOT NULL DEFAULT 1, -- 状态：0=审核中，1=正常，2=被封禁/冻结

    deleted SMALLINT DEFAULT 0, -- 0=正常，1=已删除

    -- 时间字段 (去掉了 ON UPDATE，由代码控制)
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- --- 添加注释 (PostgreSQL 标准写法) ---
COMMENT ON TABLE domains.circle IS '兴趣圈/社区表';
COMMENT ON COLUMN domains.circle.id IS '主键ID(UUIDv7)';
COMMENT ON COLUMN domains.circle.name IS '圈子名称';
COMMENT ON COLUMN domains.circle.slug IS 'URL友好的唯一标识符';
COMMENT ON COLUMN domains.circle.avatar_url IS '圈子头像URL';
COMMENT ON COLUMN domains.circle.cover_url IS '圈子封面背景图URL';
COMMENT ON COLUMN domains.circle.description IS '圈子描述';
COMMENT ON COLUMN domains.circle.rule IS '圈子规则';
COMMENT ON COLUMN domains.circle.creator_id IS '创建者用户ID(UUID)';
COMMENT ON COLUMN domains.circle.category_id IS '所属分类ID(UUID)，NULL=未分类';
COMMENT ON COLUMN domains.circle.hot IS '综合热度值';
COMMENT ON COLUMN domains.circle.member_count IS '成员总数(缓存字段)';
COMMENT ON COLUMN domains.circle.post_count IS '帖子总数(缓存字段)';
COMMENT ON COLUMN domains.circle.join_type IS '加入限制：0=公开，1=审核，2=私密';
COMMENT ON COLUMN domains.circle.status IS '圈子状态：0=待审，1=正常，2=封禁';
COMMENT ON COLUMN domains.circle.deleted IS '逻辑删除：0=未删，1=已删';
COMMENT ON COLUMN domains.circle.create_time IS '创建时间';
COMMENT ON COLUMN domains.circle.update_time IS '更新时间';

-- --- 创建索引 (Index) ---

-- 1. 唯一性约束
-- 圈子名称不允许重复，且只检测未删除的记录
CREATE UNIQUE INDEX idx_circle_name ON domains.circle(name) WHERE deleted = 0;
-- 如果使用了 slug 用于 URL 访问
CREATE UNIQUE INDEX idx_circle_slug ON domains.circle(slug) WHERE deleted = 0;
```

## 成员权限表

```sql
DROP TABLE IF EXISTS domains.circle_member;

CREATE TABLE domains.circle_member (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    circle_id UUID NOT NULL, -- 圈子ID
    user_id UUID NOT NULL, -- 用户ID

    -- 角色与权限 (RBAC核心)
    -- 10=普通成员, 20=管理员, 30=圈主 (使用间隔数字，方便未来插入"副圈主"或"嘉宾")
    role SMALLINT NOT NULL DEFAULT 10,

    -- 成员状态 (控制访问权)
    -- 0=待审核(申请中), 1=正常, 2=禁言(Muted), 3=拉黑/踢出(Banned) 4=暂时退出
    status SMALLINT NOT NULL DEFAULT 1,
    -- 成员声望 帖子/评论被点赞+1 帖子被收藏+5 帖子/评论被别人回复+10
    reputation INT NOT NULL DEFAULT 0,

    -- 禁言控制 (结合 status=2 使用)
    mute_end_time TIMESTAMPTZ, -- 禁言结束时间，NULL或过去时间代表无禁言

    -- 个性化设置 (用户对该圈子的设置)
    is_top SMALLINT DEFAULT 0, -- 是否置顶显示 (0=否, 1=是)
    is_disturb SMALLINT DEFAULT 0, -- 消息免打扰 (0=否, 1=是)

    -- 时间字段
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, -- 加入时间
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP  -- 角色/状态变更时间
);

-- --- 注释 ---
COMMENT ON TABLE domains.circle_member IS '圈子成员关系与权限表';
COMMENT ON COLUMN domains.circle_member.circle_id IS '圈子ID(UUID)';
COMMENT ON COLUMN domains.circle_member.user_id IS '用户ID(UUID)';
COMMENT ON COLUMN domains.circle_member.role IS '角色: 10=成员, 20=管理员, 30=圈主';
COMMENT ON COLUMN domains.circle_member.status IS '状态: 0=申请中, 1=正常, 2=禁言, 3=拉黑';
COMMENT ON COLUMN domains.circle_member.mute_end_time IS '禁言截止时间';

-- --- 索引优化---

-- 1. 【必须】唯一性约束 (防止重复加入)
-- 每个用户在同一个圈子只能有一条记录
CREATE UNIQUE INDEX idx_member_unique ON domains.circle_member(circle_id, user_id);

-- 2. 【核心】查询 "我加入的圈子"
-- 场景：APP "我的" 页面，或校验用户发帖权限
-- 包含 role 和 status 是为了覆盖索引(Covering Index)，查权限时不回表
CREATE INDEX idx_member_user ON domains.circle_member(user_id, role, status);

-- 3. 【管理】查询 "圈子的管理员列表" 或 "圈子成员列表"
-- 场景：圈主管理成员，或者展示成员列表
CREATE INDEX idx_member_circle_role ON domains.circle_member(circle_id, role DESC, create_time DESC);
```

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
    summary VARCHAR(500) NOT NULL DEFAULT '', -- 摘要 (纯文本，用于推送/列表预览，去除了HTML标签)
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

## 评论表

> 二级扁平化结构:`root_id` 为 NULL 表示顶层评论;回复时 `root_id` 指向所属顶层评论。
> keyset 游标分页的 `id` 字典序比较:UUIDv7 下 `id < ?` 等价「更早创建」,与 `ORDER BY id DESC` 配合正确。

```sql
DROP TABLE IF EXISTS domains.comment;

CREATE TABLE domains.comment (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 1. 归属关系
    post_id UUID NOT NULL,                    -- 所属帖子ID
    user_id UUID NOT NULL,                    -- 发布者ID

    -- 2. 结构关系 (二级扁平化)
    root_id UUID,                             -- 根评论ID (NULL=顶层，否则指向所属顶层评论ID)
    reply_to_id UUID,                         -- 被回复评论ID (NULL=非回复特定评论)
    reply_to_user_id UUID,                    -- 被回复用户ID (NULL=非回复特定用户)

    -- 3. 内容
    content TEXT NOT NULL,                       -- 评论文本内容
    extra_data JSONB DEFAULT '{}'::JSONB,        -- 扩展数据：富文本结构/图片/视频JSON (预留)

    -- 4. 统计数据 (高频更新字段)
    like_count INT NOT NULL DEFAULT 0,          -- 点赞数
    reply_count INT NOT NULL DEFAULT 0,         -- 子评论数

    -- 5. 状态与时间
    status SMALLINT NOT NULL DEFAULT 1,         -- 1=正常, 2=审核中, 3=隐藏
    deleted SMALLINT DEFAULT 0,                 -- 0=正常, 1=已删除
    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 添加表注释
COMMENT ON TABLE domains.comment IS '评论表：统一存储评论的元数据和内容，PostgreSQL TOAST机制自动处理大字段存储';

-- 添加列注释
COMMENT ON COLUMN domains.comment.id IS '主键ID(UUIDv7)';
COMMENT ON COLUMN domains.comment.post_id IS '所属帖子ID(UUID)';
COMMENT ON COLUMN domains.comment.user_id IS '评论发布者ID(UUID)';
COMMENT ON COLUMN domains.comment.root_id IS '根评论ID：NULL表示顶层评论，否则存放所属的顶层评论ID(UUID)';
COMMENT ON COLUMN domains.comment.reply_to_id IS '被回复的评论ID：NULL表示非回复特定人，否则存放被回复的那条评论ID(UUID)';
COMMENT ON COLUMN domains.comment.reply_to_user_id IS '被回复的用户id(UUID)';
COMMENT ON COLUMN domains.comment.content IS '评论文本内容';
COMMENT ON COLUMN domains.comment.extra_data IS '扩展数据：JSON格式，用于存储富文本结构、图片列表、视频链接等附加信息';
COMMENT ON COLUMN domains.comment.like_count IS '点赞数：高频更新字段，建议配合 Redis Write-Behind 策略';
COMMENT ON COLUMN domains.comment.reply_count IS '子回复数：该评论下的回复数量';
COMMENT ON COLUMN domains.comment.status IS '状态：1=正常, 2=审核中, 3=审核不通过/折叠';
COMMENT ON COLUMN domains.comment.deleted IS '逻辑删除：0=正常, 1=已删除';
COMMENT ON COLUMN domains.comment.create_time IS '创建时间';
COMMENT ON COLUMN domains.comment.update_time IS '更新时间 (如点赞、状态变更时更新)';

-- 索引设计 (根据查询模式优化)
-- 场景：查询某个帖子下的顶层评论，按点赞数量倒序 (root_id IS NULL 表示顶层)
CREATE INDEX idx_comment_post_root_like ON domains.comment (post_id, root_id, like_count DESC);
-- 场景：查询某个帖子下的顶层评论，按时间倒序
CREATE INDEX idx_comment_post_root_time ON domains.comment (post_id, root_id, create_time DESC);

-- 场景：查询用户的历史评论
CREATE INDEX idx_comment_user_time ON domains.comment (user_id, create_time DESC);

-- 场景：查询某个根评论下的所有子回复 (root_id 非空)
CREATE INDEX idx_comment_root_id ON domains.comment (root_id) WHERE root_id IS NOT NULL;
```

## 评论点赞表

```sql
-- 评论点赞表 (comment_like)
DROP TABLE IF EXISTS domains.comment_like;

CREATE TABLE domains.comment_like (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    user_id UUID NOT NULL,           -- 点赞人
    comment_id UUID NOT NULL,         -- 被点赞的评论

    -- 冗余字段 (可选，但推荐)
    post_id UUID,  -- 冗余帖子ID，NULL表示仅评论点赞未冗余，有助于查询

    -- 点赞状态 (0=有效点赞, 1=取消点赞)
    deleted SMALLINT NOT NULL DEFAULT 0,

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.comment_like IS '评论点赞流水表';
COMMENT ON COLUMN domains.comment_like.user_id IS '点赞人ID(UUID)';
COMMENT ON COLUMN domains.comment_like.comment_id IS '被点赞评论ID(UUID)';
COMMENT ON COLUMN domains.comment_like.deleted IS '点赞状态: 0=有效点赞, 1=取消点赞';
COMMENT ON COLUMN domains.comment_like.post_id IS '冗余帖子ID(UUID)，便于查询';

-- --- 索引优化 ---

-- 1. 【核心】保证每个用户对每个评论只有一条点赞/取消点赞的记录
CREATE UNIQUE INDEX uk_comment_like_user_comment ON domains.comment_like(user_id, comment_id);

-- 2. 【核心】查询"我点赞过的评论"
-- 当用户在个人中心查看"我赞过的评论"时使用。
CREATE INDEX idx_clike_user_active ON domains.comment_like(user_id, create_time DESC) WHERE deleted = 0;

-- 3. 【统计/关联】查询某评论的点赞者列表 (通常只显示头像，不常翻页)
-- 配合 `deleted=0` 查询有效点赞者。
CREATE INDEX idx_clike_comment_active ON domains.comment_like(comment_id, create_time DESC) WHERE deleted = 0;
```

## 帖子点赞表

```sql
-- 帖子点赞表 (post_like)
DROP TABLE IF EXISTS domains.post_like;

CREATE TABLE domains.post_like (
    -- ID主键 (UUIDv7)
    id UUID PRIMARY KEY DEFAULT uuidv7(),

    -- 核心关系
    user_id UUID NOT NULL,           -- 点赞人
    post_id UUID NOT NULL,           -- 帖子ID (必填)

    -- 点赞状态 (0=有效点赞, 1=取消点赞)
    deleted SMALLINT NOT NULL DEFAULT 0,

    create_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- --- 注释 ---
COMMENT ON TABLE domains.post_like IS '帖子点赞流水表';
COMMENT ON COLUMN domains.post_like.user_id IS '点赞人ID(UUID)';
COMMENT ON COLUMN domains.post_like.post_id IS '帖子ID(UUID)，便于查询';
COMMENT ON COLUMN domains.post_like.deleted IS '点赞状态: 0=有效点赞, 1=取消点赞';

-- --- 索引优化---

-- 1. 【核心】保证每个用户对每个帖子只有一条点赞/取消点赞的记录
CREATE UNIQUE INDEX uk_post_like_user_post ON domains.post_like(user_id, post_id);

-- 2. 【核心】查询"我点赞过的帖子"
-- 当用户在个人中心查看"我赞过的帖子"时使用。
CREATE INDEX idx_user_post_active ON domains.post_like(user_id, create_time DESC) WHERE deleted = 0;

-- 3. 【统计/关联】查询某帖子的点赞者列表
-- 配合 `deleted=0` 查询有效点赞者。
CREATE INDEX idx_clike_post_active ON domains.post_like(post_id, create_time DESC) WHERE deleted = 0;
```

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

# 用户域(user)

> 全局约定(UUIDv7 主键、无 FK/CHECK、逻辑删除)见 [README.md](README.md)。

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

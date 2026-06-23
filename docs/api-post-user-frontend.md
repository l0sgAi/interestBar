# 前端对接文档：查看任意用户发帖（GET /post/user/:user_id）

> 配套后端接口规范见 [api-post-user.md](api-post-user.md)。本文档面向前端开发，给出 TypeScript 类型、调用方式、游标分页实现与错误处理。

## 1. 用途

「用户主页 / 他人主页」场景：查看某个用户的发帖记录，支持关键字模糊搜索标题与摘要。

- **数据范围**：仅目标用户（路径 `:user_id`）的帖子。
- **可见性**：只返回 `status=1`（已发布）。草稿/审核中/已拒绝/已封禁帖**对查看者不可见**，前端无需处理这些状态。
- **分页**：`search_after` 游标分页（非 page/offset）。
- **响应结构**：与 [`/post/my`](api-post-my.md) 完全一致，帖子列表组件可直接复用。

## 2. 接口信息

| 项 | 值 |
|---|---|
| Method | `GET` |
| Path | `/post/user/:user_id` |
| `:user_id` | 目标用户 UUIDv7 |
| 鉴权 | **需登录**，请求头 `satoken: <token>` |
| Content-Type | 无 body，query 参数走 URL |

> 网关若有全局前缀，实际为 `/api/v1/post/user/:user_id`，以部署为准。
> `satoken` 请求头名由后端 `sa_token.token_name` 配置决定，当前固定 `satoken`，值取登录接口返回的 token。

## 3. 请求参数

### 路径参数

| 参数 | 类型 | 必填 | 校验 |
|---|---|---|---|
| `user_id` | `string` (UUIDv7) | 是 | 非法 UUID → `400 Invalid user_id` |

### Query 参数

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `keyword` | `string` | 否 | `""` | 模糊匹配 `title`（权重×3）+ `summary`（权重×1），支持拼写容错。空 → 返回该用户全部已发布帖 |
| `size` | `number` | 否 | `20` | 每页条数。`<=0` 或 `>100` 时后端回退为 `20` |
| `search_after` | `string` | 否 | `""` | 上一页响应的 `data.search_after`，**原样透传**（是个 JSON 数组字符串） |

### TypeScript 请求类型

```ts
export interface GetUserPostsParams {
  /** 关键字，空字符串表示不筛选 */
  keyword?: string;
  /** 每页条数，建议 10~50，超过 100 后端回退 20 */
  size?: number;
  /** 上一页响应返回的游标；首页不传 / 传空串 */
  search_after?: string;
}
```

## 4. 响应

### 外层结构（标准响应包）

```jsonc
{
  "code": 200,            // 业务码：200 成功；201 参数错；202 鉴权失败；210 服务端错
  "message": "Success",
  "data": { /* PostSearchResult */ }
}
```

### `data`（PostSearchResult）

| 字段 | 类型 | 说明 |
|---|---|---|
| `posts` | `PostListItem[]` | 帖子列表（已含作者/圈子/图片） |
| `total` | `number` | 命中总数 |
| `size` | `number` | 本页实际返回条数 |
| `search_after` | `string` | 下一页游标；**空串 `""` = 已到末页** |

### TypeScript 响应类型

```ts
/** 帖子类型 */
export enum PostType {
  Image = 1, // 图文
  Video = 2, // 视频
  Vote = 3,  // 投票
}

/** 帖子列表项 —— 与 /post/my 完全一致，列表组件可复用 */
export interface PostListItem {
  id: string;            // uuid
  circle_id: string;     // uuid，所属圈子
  user_id: string;       // uuid，恒为路径传入的目标用户
  type: PostType;
  title: string;
  summary: string;
  content: string;
  view_count: number;
  comment_count: number;
  like_count: number;
  collect_count: number;
  is_pinned: 0 | 1;
  is_essence: 0 | 1;
  is_lock: 0 | 1;
  status: 1;             // 本接口恒为 1（已发布），无需处理其它状态
  create_time: string;   // RFC3339Nano，如 "2026-06-20T10:23:45.123456789Z"
  author_name: string;
  author_avatar: string;
  circle_name: string;
  circle_avatar: string;
  images: string[];
}

export interface PostSearchResult {
  posts: PostListItem[];
  total: number;
  size: number;
  search_after: string;  // "" 表示无下一页
}

/** 标准响应包 */
export interface ApiResponse<T> {
  code: number;       // 200 成功
  message: string;
  data: T;
}
```

## 5. 调用示例（axios）

建议用项目统一的 axios 实例，`satoken` 与 baseURL 在拦截器里配好：

```ts
// request.ts —— 统一实例（已存在则复用）
import axios from 'axios';

export const http = axios.create({
  baseURL: '/api/v1', // 按部署调整
  timeout: 10000,
});

// 请求拦截：自动带 satoken
http.interceptors.request.use((config) => {
  const token = localStorage.getItem('satoken'); // 或你的登录态存储
  if (token) config.headers['satoken'] = token;
  return config;
});
```

```ts
// api/post.ts
import { http } from '@/request';
import type { ApiResponse, GetUserPostsParams, PostSearchResult } from './types';

export function getUserPosts(
  userId: string,
  params: GetUserPostsParams = {},
): Promise<PostSearchResult> {
  return http
    .get<ApiResponse<PostSearchResult>>(`/post/user/${userId}`, { params })
    .then((res) => {
      const body = res.data;
      if (body.code !== 200) {
        // 业务错误（201/202/210），交给调用方或全局拦截器处理
        return Promise.reject(new Error(body.message));
      }
      return body.data;
    });
}
```

## 6. 游标分页实现

核心规则：**首页不带 `search_after`；每页把响应的 `data.search_after` 原样传给下一页；当其为空串时停止。**

> ⚠️ `search_after` 是个 JSON 数组字符串（如 `["0192f8a1-...-..."]`），**不要解析、不要修改、不要重新序列化**，直接当 query 透传（axios 会自动 URL-encode）。翻页中途**不要换** `keyword` / `size`，否则游标失效。

### React + 无限滚动 / 加载更多

```tsx
import { useCallback, useEffect, useState } from 'react';
import { getUserPosts } from '@/api/post';
import type { PostListItem } from '@/api/post';

export function useUserPosts(userId: string, keyword = '') {
  const [posts, setPosts] = useState<PostListItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cursor, setCursor] = useState<string>('');
  const [hasMore, setHasMore] = useState(true);

  // keyword 或 userId 变化 → 重置，从首页拉
  const reset = useCallback(() => {
    setPosts([]);
    setTotal(0);
    setCursor('');
    setHasMore(true);
    setError(null);
  }, []);

  const loadMore = useCallback(async () => {
    if (loading || !hasMore) return;
    setLoading(true);
    setError(null);
    try {
      const data = await getUserPosts(userId, {
        keyword,
        size: 20,
        search_after: cursor || undefined, // 首页不传
      });
      setPosts((prev) => [...prev, ...data.posts]);
      setTotal(data.total);
      const next = data.search_after;
      setCursor(next);
      setHasMore(next !== ''); // 空串 = 末页
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [userId, keyword, cursor, loading, hasMore]);

  useEffect(() => {
    reset();
    // reset 后 cursor 回到 ''，触发首页加载
  }, [userId, keyword, reset]);

  // cursor 清空时自动拉首页（reset 后）
  useEffect(() => {
    if (cursor === '' && posts.length === 0 && hasMore) loadMore();
  }, [cursor, posts.length, hasMore, loadMore]);

  return { posts, total, loading, error, hasMore, loadMore, reload: reset };
}
```

### Vue 3 组合式（等价思路）

```ts
import { ref, watch } from 'vue';

export function useUserPosts(userId: Ref<string>, keyword: Ref<string>) {
  const posts = ref<PostListItem[]>([]);
  const total = ref(0);
  const loading = ref(false);
  const hasMore = ref(true);
  const cursor = ref('');

  async function loadMore() {
    if (loading.value || !hasMore.value) return;
    loading.value = true;
    try {
      const data = await getUserPosts(userId.value, {
        keyword: keyword.value,
        size: 20,
        search_after: cursor.value || undefined,
      });
      posts.value.push(...data.posts);
      total.value = data.total;
      cursor.value = data.search_after;
      hasMore.value = data.search_after !== '';
    } finally {
      loading.value = false;
    }
  }

  // userId/keyword 变 → 重置
  watch([userId, keyword], () => {
    posts.value = []; total.value = 0; cursor.value = ''; hasMore.value = true;
    loadMore();
  }, { immediate: true });
}
```

## 7. 错误处理

后端同时返回 **HTTP 状态码** 和 **业务码 `code`**。axios 默认对 HTTP 4xx/5xx 走 reject；2xx 时需再判 `body.code === 200`。

| HTTP | `code` | `message` | 前端处理建议 |
|---|---|---|---|
| 401 | 202 | `Token not found` | 跳登录页 |
| 401 | 202 | `Invalid or expired token` | 清登录态 → 跳登录页 |
| 400 | 201 | `Invalid user_id` | 路径参数非法，通常是路由拼错；提示 + 回退 |
| 400 | 201 | `Invalid search_after parameter` | 游标被改坏；重置翻页从首页重拉 |
| 500 | 210 | `Failed to search user posts` | 服务端异常；toast「加载失败，请重试」 |

建议在 axios 响应拦截器统一处理 401：

```ts
http.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('satoken');
      location.href = '/login';
    }
    return Promise.reject(err);
  },
);
```

## 8. 注意事项

1. **草稿不可见是正常的**：本接口只返回 `status=1`。目标用户的草稿/审核/拒绝/封禁帖前端拿不到，**不要**在 UI 上展示「无草稿」之类的提示。看自己全部状态（含草稿）请用 `/post/my`。
2. **计数有秒级延迟**：`view_count` / `like_count` 取自 ES 快照，异步落库，点赞后可能短暂不一致，属正常。
3. **空结果**：用户无已发布帖时返回 `posts: [], total: 0, search_after: ""`，UI 显示「暂无帖子」空状态即可。
4. **查本人**：`user_id` 传当前登录用户 ID 时等价于「只看自己已发布帖」（草稿仍不可见）。
5. **`search_after` 翻页稳定性**：游标绑定当时的 `keyword`/`size`/排序，翻页期间勿变更这些参数；如需换搜索词，重置列表从首页开始。
6. **URL-encode**：`search_after` 含 `"`、`[`、`]`，axios 传 query 会自动编码；若手拼 URL 记得 `encodeURIComponent`。

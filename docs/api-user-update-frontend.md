# 前端对接文档：修改个人资料 / 重置密码（PUT /user/update）

> 面向前端开发。本文档重点说明**本次改造新增的密码重置能力**，并给出 TypeScript 类型、调用方式与错误处理。
>
> 该接口为**部分更新（PATCH 语义）**：只传哪个字段就改哪个；不传的字段保持不变。

## 1. 用途

「个人中心 / 设置」场景，已登录用户修改自己的资料，并可同时（或单独）重置密码。

- **鉴权**：需登录，请求头 `satoken: <token>`。
- **部分更新**：所有字段均为可选，按需提交；**至少传一个字段**，否则报错。
- **本次新增**：支持重置密码 —— 新增 `password` + `confirm_password` 两个字段。
- **纯重置语义**：**不校验旧密码**。任何已登录用户（含纯 OAuth 注册、原本无密码的用户）可直接设置新密码。

## 2. 接口信息

| 项 | 值 |
|---|---|
| Method | `PUT` |
| Path | `/user/update` |
| 鉴权 | **需登录**，请求头 `satoken: <token>` |
| Content-Type | `application/json` |

> 网关若有全局前缀，实际为 `/api/v1/user/update`，以部署为准。
> `satoken` 请求头名由后端 `sa_token.token_name` 配置决定，当前固定 `satoken`，值取登录接口返回的 token。

## 3. 请求字段

所有字段均可选（部分更新）。字段分两组：**资料字段**（原有）与**密码字段**（🆕 本次新增）。

### 资料字段（原有）

| 字段 | 类型 | 必填 | 校验 | 说明 |
|---|---|---|---|---|
| `username` | `string` | 否 | 非空，长度 1~50 | 用户名/昵称；传空串 → 400 |
| `avatar_url` | `string` | 否 | 合法 URL | 头像地址 |
| `phone` | `string` | 否 | — | 手机号；传空串表示**清空** |
| `gender` | `number` | 否 | 0~3 | 0=未知 1=男 2=女 3=其它 |
| `birthdate` | `string\|null` | 否 | 不能是未来日期 | RFC3339，如 `"2000-01-01T00:00:00Z"` |

### 密码字段（🆕 本次新增）

| 字段 | 类型 | 必填 | 校验 | 说明 |
|---|---|---|---|---|
| `password` | `string` | 否* | 最小 6 字符 | 新密码；**不 trim**，空格是有意义字符 |
| `confirm_password` | `string` | 否* | 须与 `password` 完全相等 | 确认密码 |

> *密码字段**成对约束**：要么都传，要么都不传。只传其中一个 → 400。

### TypeScript 请求类型

```ts
/** 修改资料 / 重置密码入参。所有字段可选（部分更新） */
export interface UpdateProfileRequest {
  // ---- 资料字段（原有）----
  username?: string;
  avatar_url?: string;
  phone?: string;          // 传空串 "" 表示清空手机号
  gender?: 0 | 1 | 2 | 3;
  birthdate?: string | null; // RFC3339；不可为未来日期

  // ---- 密码字段（本次新增）----
  /** 新密码（最小 6 字符，不 trim） */
  password?: string;
  /** 确认密码，必须与 password 完全一致 */
  confirm_password?: string;
}
```

## 4. 密码重置规则（重点）

| 场景 | 入参 | 结果 |
|---|---|---|
| 不改密码 | 两者都不传 | 跳过密码逻辑，正常改资料 |
| 只改密码 | 两者都传 + 合法 | 仅写 `pwd`，其他资料不变 |
| 同时改资料+密码 | 都传 | 一次请求同时更新 |
| 只传一个 | 仅 `password` 或仅 `confirm_password` | ❌ 400 `password and confirm_password must be provided together` |
| 过短 | 长度 < 6 | ❌ 400 `Password must be at least 6 characters` |
| 不一致 | 两者不等 | ❌ 400 `Password and confirm password do not match` |

关键约定：

1. **不校验旧密码**：本接口是「重置」语义，无需 `old_password`。
2. **首次设密**：纯 OAuth 注册、原本 `pwd` 为空的用户，也可通过本接口**首次设置**一个密码（之后即可走邮箱密码登录）。
3. **密码不 trim**：后端不做首尾去空格，前端输入框也**不要**自行 `trim()`，否则可能与用户预期不符。
4. **会话保持**：改密成功后**不会**踢出当前会话，当前 `satoken` 继续有效，无需重新登录。（如产品要求改密后强制重登，需后端额外支持，当前未实现。）
5. **响应不含密码**：`pwd` 哈希永远不出现在响应里（字段 `json:"-"`）。

## 5. 响应

### 外层结构（标准响应包）

```jsonc
{
  "code": 200,            // 200 成功；201 参数错；202 鉴权失败；210 服务端错
  "message": "Profile updated successfully",
  "data": { /* UpdateProfileResult */ }
}
```

### `data`（UpdateProfileResult）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | 用户 UUIDv7 |
| `username` | `string` | 用户名 |
| `email` | `string` | 邮箱 |
| `avatar_url` | `string` | 头像 |
| `phone` | `string` | 手机号 |
| `gender` | `number` | 0/1/2/3 |
| `birthdate` | `string\|null` | 生日 RFC3339，未设置时为 `null` |

> 返回的是**更新后的最新资料快照**，可直接用它刷新本地用户状态。**注意：无论是否改密码，响应结构完全一致，绝无 `pwd` 字段。**

### TypeScript 响应类型

```ts
export interface UpdateProfileResult {
  id: string;
  username: string;
  email: string;
  avatar_url: string;
  phone: string;
  gender: number;
  birthdate: string | null;
}

/** 标准响应包 */
export interface ApiResponse<T> {
  code: number;       // 200 成功
  message: string;
  data: T;
}
```

## 6. 调用示例（axios）

建议复用项目统一 axios 实例（`satoken` 与 baseURL 在拦截器配好）。

```ts
// api/user.ts
import { http } from '@/request';
import type { ApiResponse, UpdateProfileRequest, UpdateProfileResult } from './types';

export function updateProfile(
  payload: UpdateProfileRequest,
): Promise<UpdateProfileResult> {
  return http
    .put<ApiResponse<UpdateProfileResult>>('/user/update', payload)
    .then((res) => {
      const body = res.data;
      if (body.code !== 200) {
        return Promise.reject(new Error(body.message));
      }
      return body.data;
    });
}
```

### 场景 A：只改资料（与改造前完全一致）

```ts
await updateProfile({
  username: '新昵称',
  gender: 2,
  birthdate: '2000-01-01T00:00:00Z',
});
```

### 场景 B：只重置密码（🆕）

```ts
await updateProfile({
  password: 'newPass123',
  confirm_password: 'newPass123',
});
```

### 场景 C：改资料 + 重置密码一次性提交

```ts
await updateProfile({
  avatar_url: 'https://cdn.example.com/a.png',
  password: 'newPass123',
  confirm_password: 'newPass123',
});
```

### 前端表单联动（React 示例）

确认密码通常与密码输入框联动校验，提交前本地先判一次，减少 400 往返：

```tsx
function ResetPasswordForm() {
  const [pwd, setPwd] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');

  async function submit() {
    if (pwd.length < 6) return setError('密码至少 6 位');
    if (pwd !== confirm) return setError('两次密码不一致');
    setError('');
    try {
      await updateProfile({ password: pwd, confirm_password: confirm });
      // 改密成功；会话仍有效，无需跳登录
      toast.success('密码已更新');
    } catch (e) {
      setError((e as Error).message); // 直接显示后端 message
    }
  }
  // ...inputs...
}
```

## 7. 错误处理

后端同时返回 **HTTP 状态码** 与 **业务码 `code`**。axios 默认对 HTTP 4xx/5xx 走 reject；2xx 时需再判 `body.code === 200`。

| HTTP | `code` | `message` | 触发条件 | 前端处理建议 |
|---|---|---|---|---|
| 401 | 202 | `Token not found` | 未带/失效 token | 清登录态 → 跳登录页 |
| 400 | 201 | `Invalid request parameters` | body 非 JSON / 字段类型错 | 提示「请求参数有误」 |
| 400 | 201 | `At least one field must be provided` | 一个字段都没传 | 引导用户至少填一项 |
| 400 | 201 | `Username cannot be empty` | `username` 传了空串 | 提示用户名不能为空 |
| 400 | 201 | `Gender must be 0 (unknown)...` | `gender` 越界（前端枚举已挡） | 一般不会到这 |
| 400 | 201 | `Birthdate cannot be in the future` | 生日是未来日期 | 日期选择器限制即可 |
| 400 | 201 | `password and confirm_password must be provided together` | 🆕 只传了其中一个密码字段 | 提示「请同时填写并确认密码」 |
| 400 | 201 | `Password must be at least 6 characters` | 🆕 长度 < 6 | 提示「密码至少 6 位」 |
| 400 | 201 | `Password and confirm password do not match` | 🆕 两次不一致 | 提示「两次输入不一致」 |
| 500 | 210 | `Failed to update user info` | 服务端异常 | toast「保存失败，请重试」 |

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

1. **密码字段是本次唯一新增**：老前端的「修改资料」表单无需改动即可继续工作；只有新增「修改密码 / 设置密码」入口时才需要传 `password` + `confirm_password`。
2. **「设置密码」入口**：对于从未设过密码的 OAuth 用户，可在个人中心提供「设置密码」按钮，复用同一接口（无需区分「首次设置」与「重置」）。
3. **不要 trim 密码**：密码框输入请原样提交，包括首尾空格。
4. **会话不失效**：改密成功后当前 token 仍可用，**不要**自动把用户踢去登录页（除非产品明确要求）。
5. **HTTPS**：本接口承载明文密码（传输层加密依赖 HTTPS），生产务必走 HTTPS。
6. **不要把密码写进 URL / 日志**：本接口是 `PUT` body，密码在请求体中；前端排查时避免把请求体打印到日志或上报。
7. **响应复用**：成功响应的 `data` 是更新后的完整资料，可直接覆盖本地用户对象刷新 UI。

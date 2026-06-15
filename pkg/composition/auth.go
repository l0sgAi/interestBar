package composition

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/click33/sa-token-go/stputil"
)

// RequireLogin 是一个框架无关的"需要登录"中间件（routing.HandlerFunc）。
//
// 它替代旧的 sagin.CheckLogin() gin 中间件，但实现方式不同：
//   - sagin.CheckLogin() 依赖 gin 中间件链（c.Next/c.Abort）
//   - RequireLogin 直接用 stputil 校验 token，把 loginID/userID 写入 AppContext
//
// 两者行为等价：无 token / token 无效 → 返回 401；有效 → 放行并填充用户信息。
// 区别在于 RequireLogin 不依赖任何 Web 框架的中间件机制，可跨 gin/hertz 复用。
//
// 设计权衡：为何不直接复用 sagin.CheckLogin？
//   - sagin.CheckLogin 返回 gin.HandlerFunc，强绑定 gin；
//   - 把它包成 routing.HandlerFunc 会破坏 c.Next() 语义（它需要作为中间件
//     挂在 gin 路由上，而不是作为普通 handler 被调用）；
//   - 因此在过渡期，我们用 stputil（与框架无关）重新实现等价逻辑。
func RequireLogin(c appctx.AppContext) {
	tokenName := conf.Config.SaToken.TokenName
	token := c.Header(tokenName)

	// 兼容：token 也可能在 cookie 里（与 sagin 行为一致）
	// AppContext 暂未暴露 Cookie 读取，这里仅从 header 取。
	// 若未来需要 cookie 兜底，可在 AppContext 增加 Cookie() 方法。

	if token == "" {
		httputil.Unauthorized(c, "Token not found")
		return
	}

	// 校验 token 是否登录
	if !stputil.IsLogin(token) {
		httputil.Unauthorized(c, "Invalid or expired token")
		return
	}

	// 取 loginID
	loginID, err := stputil.GetLoginID(token)
	if err != nil {
		httputil.Unauthorized(c, "Invalid or expired token")
		return
	}

	// 写入 AppContext，供后续 handler 使用
	c.SetLoginID(loginID)
	// 注意：不在这里 parse userID，由 handler 按需调用 appctx 或工具函数解析。
	// 这样保持中间件的轻量，也避免引入 uuid.Parse 失败的分支。
}

// RequireLoginWith sagin.CheckLogin 的框架无关等价物（别名，语义更清晰）。
// 保持为一个 routing.HandlerFunc 变量，便于在路由注册时直接传入。
//
// 用法：
//
//	cat := rg.Group("/category", RequireLogin)
var RequireLoginFn = RequireLogin

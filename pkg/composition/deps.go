// Package composition 是组装层：把各领域（domains/*）的依赖装配起来，
// 并把它们的路由挂到 Web server 上。
//
// 这是"模块化单体"的核心编排点：
//   - 它知道所有领域（import 各领域的 application/http 包）；
//   - 它持有基础设施资源（DB/Redis 连接），构造 Repository 注入给 Service；
//   - 领域之间通过接口（Facade）相互调用时，绑定的实现也在这里决定。
//
// 未来拆分微服务时，把某个领域抽出去，只需复制领域包 + 在新服务里
// 重写一份 composition（可能注入 RPC client 代替本地 Repository）。
package composition

import (
	"interestBar/pkg/server/storage/db/pgsql"
)

// Deps 聚合所有领域 Service 的依赖（基础设施资源）。
//
// 目前只含 *gorm.DB；随着更多领域搬迁，会逐步补充 Redis client、
// ES client、Redpanda producer 等资源。
type Deps struct {
	DB *pgsql.DBHolder
}

// NewDeps 从项目现有的全局单例构造 Deps。
//
// 注意：这里暂时复用 pgsql.DB 全局变量，作为"过渡期"依赖来源。
// 待所有领域搬迁完成后，会把连接管理统一收口到 composition 层，
// 移除全局单例（详见 refactor-1 文档阶段 3）。
func NewDeps() *Deps {
	return &Deps{
		DB: &pgsql.DBHolder{},
	}
}

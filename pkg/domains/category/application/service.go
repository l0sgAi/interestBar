// Package application 提供 category 领域的应用服务层。
//
// 职责：用例编排（缓存策略、调用 Repository），不关心 HTTP 层细节，
// 也不关心 Repository 的具体存储实现。
//
// HTTP handler 层（interfaces/http）调用本层的 Service 完成业务。
package application

import (
	"context"

	"interestBar/pkg/domains/category/domain"
)

// CategorySimpleVO 分类简化视图对象（返回给 HTTP 层）。
//
// 与旧 controller.CategorySimpleResponse 一致：只暴露前端需要的字段。
type CategorySimpleVO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Sort int    `json:"sort"`
}

// CategoryService 是 category 领域的应用服务接口。
//
// 定义为接口便于：
//   - composition 层注入不同实现（本地 / 未来 RPC client）
//   - HTTP handler 层 mock 测试
type CategoryService interface {
	// GetCategories 获取分类列表（带缓存）。
	GetCategories(ctx context.Context) ([]CategorySimpleVO, error)
}

// categoryServiceImpl 是 CategoryService 的默认实现。
type categoryServiceImpl struct {
	repo  domain.CategoryRepository
	cache domain.CategoryCache
}

// NewCategoryService 构造一个 CategoryService。
// repo/cache 由 composition 层注入具体实现。
func NewCategoryService(repo domain.CategoryRepository, cache domain.CategoryCache) CategoryService {
	return &categoryServiceImpl{repo: repo, cache: cache}
}

// GetCategories 获取分类列表：先查缓存，未命中回源 DB 并回写缓存。
//
// 行为与旧 controller.CategoryController.GetCategories 完全一致。
func (s *categoryServiceImpl) GetCategories(ctx context.Context) ([]CategorySimpleVO, error) {
	// 1. 尝试从缓存获取
	cached, _ := s.cache.GetCategories(ctx)
	if len(cached) > 0 {
		return toSimpleVOs(cached), nil
	}

	// 2. 缓存未命中，从数据库查询
	categories, err := s.repo.GetAllActiveCategories(ctx)
	if err != nil {
		return nil, err
	}

	// 3. 将结果写入缓存（失败不影响主流程）
	_ = s.cache.SetCategories(ctx, categories)

	// 4. 转换为 VO 返回
	return toSimpleVOs(categories), nil
}

// toSimpleVOs 把 domain.Category 列表转换为 CategorySimpleVO 列表。
// 注意：ID 从 uuid.UUID 转成 string，与旧 controller 的 JSON 输出格式一致。
func toSimpleVOs(categories []domain.Category) []CategorySimpleVO {
	result := make([]CategorySimpleVO, 0, len(categories))
	for i := range categories {
		result = append(result, CategorySimpleVO{
			ID:   categories[i].ID.String(),
			Name: categories[i].Name,
			Sort: categories[i].Sort,
		})
	}
	return result
}

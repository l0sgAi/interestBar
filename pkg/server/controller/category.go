package controller

import (
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/redis"
	"time"

	"github.com/gin-gonic/gin"
)

// CategoryController 处理分类相关操作
type CategoryController struct{}

func NewCategoryController() *CategoryController {
	return &CategoryController{}
}

// CategorySimpleResponse 分类简化响应结构
type CategorySimpleResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Sort int    `json:"sort"`
}

// GetCategories 获取分类列表
// GET /category/get
func (ctrl *CategoryController) GetCategories(c *gin.Context) {
	// Redis 缓存键
	cacheKey := "categories:all"

	// 1. 尝试从 Redis 获取缓存
	var categories []CategorySimpleResponse
	err := redis.GetJSON(cacheKey, &categories)
	if err == nil {
		// 缓存命中，直接返回
		response.Success(c, categories)
		return
	}

	// 2. 缓存未命中，从数据库查询
	fullCategories, err := model.GetAllActiveCategories(pgsql.DB)
	if err != nil {
		response.InternalError(c, "Failed to get categories")
		return
	}

	// 3. 转换为简化结构
	categories = make([]CategorySimpleResponse, 0, len(fullCategories))
	for _, cat := range fullCategories {
		categories = append(categories, CategorySimpleResponse{
			ID:   cat.ID,
			Name: cat.Name,
			Sort: cat.Sort,
		})
	}

	// 4. 将结果写入 Redis 缓存（缓存1小时）
	if err := redis.SetJSON(cacheKey, categories, 1*time.Hour); err != nil {
		// 缓存写入失败不影响接口返回
	}

	// 5. 返回数据
	response.Success(c, categories)
}

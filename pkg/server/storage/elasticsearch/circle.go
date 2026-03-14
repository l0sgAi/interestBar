package elasticsearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"interestBar/pkg/logger"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// CircleDocument 圈子文档结构
type CircleDocument struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description"`
	Hot         int    `json:"hot"`
	CategoryID  int    `json:"category_id"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	CreateTime  string `json:"create_time"` // 使用ISO 8601格式字符串
	Status      int16  `json:"status"`
	Deleted     int16  `json:"deleted"`
	JoinType    int16  `json:"join_type"`
	// 排序值（用于 search_after 分页）
	SortValues []interface{} `json:"sort_values,omitempty"`
}

// CircleListResponse 圈子列表响应
type CircleListResponse struct {
	Circles     []CircleDocument `json:"circles"`
	Total       int64            `json:"total"`
	Size        int              `json:"size"`
	SearchAfter []interface{}    `json:"search_after,omitempty"` // 用于获取下一页
}

// IndexCircleDoc 同步圈子文档到 ES
func IndexCircleDoc(circleID int64, name string, avatarURL string, description string, hot int, categoryID int, memberCount int, postCount int, createTime string, status int16, deleted int16, joinType int16) error {
	doc := CircleDocument{
		ID:          circleID,
		Name:        name,
		Slug:        "", // 暂时为空，需要时可以从数据库获取
		AvatarURL:   avatarURL,
		Description: description,
		Hot:         hot,
		CategoryID:  categoryID,
		MemberCount: memberCount,
		PostCount:   postCount,
		CreateTime:  createTime,
		Status:      status,
		Deleted:     deleted,
		JoinType:    joinType,
	}

	docJSON, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal document: %w", err)
	}

	req := bytes.NewReader(docJSON)

	// 使用 _create API 创建文档（如果已存在会失败）
	res, err := Client.Index(
		GetCircleIndexName(),
		req,
		Client.Index.WithDocumentID(fmt.Sprintf("%d", circleID)),
		Client.Index.WithRefresh("false"),
		Client.Index.WithOpType("index"),
	)
	if err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch error when indexing document: %s", res.String())
	}

	logger.Log.Info(fmt.Sprintf("Circle %d indexed successfully", circleID))
	return nil
}

// UpdateCircle 更新圈子文档
func UpdateCircle(circleID int64, name string, avatarURL string, description string, hot int, categoryID int, memberCount int, postCount int, createTime string, status int16, deleted int16, joinType int16) error {
	doc := CircleDocument{
		ID:          circleID,
		Name:        name,
		Slug:        "", // 暂时为空，需要时可以从数据库获取
		AvatarURL:   avatarURL,
		Description: description,
		Hot:         hot,
		CategoryID:  categoryID,
		MemberCount: memberCount,
		PostCount:   postCount,
		CreateTime:  createTime,
		Status:      status,
		Deleted:     deleted,
		JoinType:    joinType,
	}

	docJSON, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal document: %w", err)
	}

	req := bytes.NewReader(docJSON)

	res, err := Client.Update(
		GetCircleIndexName(),
		fmt.Sprintf("%d", circleID),
		req,
		Client.Update.WithRefresh("false"),
	)
	if err != nil {
		return fmt.Errorf("failed to update document: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch error when updating document: %s", res.String())
	}

	logger.Log.Info(fmt.Sprintf("Circle %d updated successfully", circleID))
	return nil
}

// DeleteCircle 删除圈子文档
func DeleteCircle(circleID int64) error {
	res, err := Client.Delete(
		GetCircleIndexName(),
		fmt.Sprintf("%d", circleID),
		Client.Delete.WithRefresh("false"),
	)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("elasticsearch error when deleting document: %s", res.String())
	}

	logger.Log.Info(fmt.Sprintf("Circle %d deleted successfully", circleID))
	return nil
}

// SearchCircles 搜索圈子
// keyword: 搜索关键字，为空时返回所有符合条件的圈子，优先使用 name 字段检索，其次使用 description 字段检索
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：圈子列表响应（包含圈子列表、总数、分页信息）
func SearchCircles(keyword string, size int, searchAfter []interface{}) (*CircleListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	// 构建搜索查询
	var searchQuery map[string]interface{}

	// 定义排序规则：按 hot、member_count、post_count、create_time 倒序
	sortRules := []map[string]interface{}{
		{
			"hot": map[string]interface{}{
				"order": "desc",
			},
		},
		{
			"member_count": map[string]interface{}{
				"order": "desc",
			},
		},
		{
			"post_count": map[string]interface{}{
				"order": "desc",
			},
		},
		{
			"create_time": map[string]interface{}{
				"order": "desc",
			},
		},
	}

	if keyword == "" {
		// 无关键字时，返回所有符合条件的圈子，按热度排序
		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
					"must_not": []map[string]interface{}{
						{
							"term": map[string]interface{}{
								"join_type": 2, // 过滤掉私密圈子
							},
						},
					},
				},
			},
			"size": size,
			"sort": sortRules,
		}
	} else {
		// 有关键字时，使用 multi_match 进行加权搜索
		// name 权重是 description 的 3 倍
		sortWithScore := []map[string]interface{}{
			{
				"_score": map[string]interface{}{
					"order": "desc",
				},
			},
		}
		sortWithScore = append(sortWithScore, sortRules...)

		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"multi_match": map[string]interface{}{
								"query":    keyword,
								"fields":   []string{"name^3", "description^1"},
								"type":     "best_fields",
								"operator": "or",
							},
						},
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
					"must_not": []map[string]interface{}{
						{
							"term": map[string]interface{}{
								"join_type": 2, // 过滤掉私密圈子
							},
						},
					},
				},
			},
			"size": size,
			"sort": sortWithScore,
		}
	}

	// 添加 search_after 参数（如果提供）
	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	res, err := Client.Search(
		Client.Search.WithContext(nil),
		Client.Search.WithIndex(GetCircleIndexName()),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
		Client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch search error: %s", res.String())
	}

	return parseCircleSearchResponse(res, size)
}

// SearchMyCircles 搜索我加入的圈子
// circleIDs: 圈子ID列表（必须）
// keyword: 搜索关键字，为空时不过滤关键词
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：圈子列表响应（包含圈子列表、总数、分页信息）
func SearchMyCircles(circleIDs []int64, keyword string, size int, searchAfter []interface{}) (*CircleListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	// 如果没有圈子ID，返回空结果
	if len(circleIDs) == 0 {
		return &CircleListResponse{
			Circles: []CircleDocument{},
			Total:   0,
			Size:    size,
		}, nil
	}

	// 构建搜索查询
	var searchQuery map[string]interface{}

	// 定义排序规则：按 create_time 倒序
	sortRules := []map[string]interface{}{
		{
			"create_time": map[string]interface{}{
				"order": "desc",
			},
		},
	}

	// 将 circleIDs 转换为 []interface{} 供ES查询使用
	circleIDInterface := make([]interface{}, len(circleIDs))
	for i, id := range circleIDs {
		circleIDInterface[i] = id
	}

	if keyword == "" {
		// 无关键字时，只按圈子ID列表过滤
		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"terms": map[string]interface{}{
								"id": circleIDInterface, // 只返回已加入的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
				},
			},
			"size": size,
			"sort": sortRules,
		}
	} else {
		// 有关键字时，使用 multi_match 进行加权搜索
		// name 权重是 description 的 3 倍
		sortWithScore := []map[string]interface{}{
			{
				"_score": map[string]interface{}{
					"order": "desc",
				},
			},
		}
		sortWithScore = append(sortWithScore, sortRules...)

		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": []map[string]interface{}{
						{
							"terms": map[string]interface{}{
								"id": circleIDInterface, // 只返回已加入的圈子
							},
						},
						{
							"multi_match": map[string]interface{}{
								"query":    keyword,
								"fields":   []string{"name^3", "description^1"},
								"type":     "best_fields",
								"operator": "or",
							},
						},
						{
							"term": map[string]interface{}{
								"status": 1, // 只返回正常状态的圈子
							},
						},
						{
							"term": map[string]interface{}{
								"deleted": 0, // 过滤掉已删除的圈子
							},
						},
					},
				},
			},
			"size": size,
			"sort": sortWithScore,
		}
	}

	// 添加 search_after 参数（如果提供）
	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	res, err := Client.Search(
		Client.Search.WithContext(nil),
		Client.Search.WithIndex(GetCircleIndexName()),
		Client.Search.WithBody(bytes.NewReader(queryJSON)),
		Client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch search error: %s", res.String())
	}

	return parseCircleSearchResponse(res, size)
}

// parseCircleSearchResponse 解析圈子搜索响应
func parseCircleSearchResponse(res *esapi.Response, size int) (*CircleListResponse, error) {
	var searchResult map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	hits := searchResult["hits"].(map[string]interface{})
	total := int64(hits["total"].(map[string]interface{})["value"].(float64))
	hitsList := hits["hits"].([]interface{})

	documents := make([]CircleDocument, 0, len(hitsList))
	var nextSearchAfter []interface{}

	for _, hit := range hitsList {
		hitMap := hit.(map[string]interface{})
		source := hitMap["_source"]
		sourceMap := source.(map[string]interface{})

		// 获取排序值（用于下一页）
		if sortArr, ok := hitMap["sort"].([]interface{}); ok {
			if len(sortArr) > 0 {
				// 记录最后一个文档的排序值
				nextSearchAfter = sortArr
			}
		}

		// 辅助函数：安全地从map中获取字符串值
		getString := func(key string) string {
			if val, ok := sourceMap[key]; ok && val != nil {
				if str, ok := val.(string); ok {
					return str
				}
			}
			return ""
		}

		// 辅助函数：安全地从map中获取整数值
		getInt := func(key string) int {
			if val, ok := sourceMap[key]; ok && val != nil {
				if num, ok := val.(float64); ok {
					return int(num)
				}
			}
			return 0
		}

		// 辅助函数：安全地从map中获取int16值
		getInt16 := func(key string) int16 {
			if val, ok := sourceMap[key]; ok && val != nil {
				if num, ok := val.(float64); ok {
					return int16(num)
				}
			}
			return 0
		}

		doc := CircleDocument{
			ID:          int64(sourceMap["id"].(float64)),
			Name:        sourceMap["name"].(string),
			Slug:        getString("slug"),
			AvatarURL:   getString("avatar_url"),
			Description: getString("description"),
			Hot:         getInt("hot"),
			CategoryID:  getInt("category_id"),
			MemberCount: getInt("member_count"),
			PostCount:   getInt("post_count"),
			CreateTime:  getString("create_time"),
			Status:      getInt16("status"),
			Deleted:     getInt16("deleted"),
			JoinType:    getInt16("join_type"),
		}
		documents = append(documents, doc)
	}

	// 如果有更多结果，返回 search_after 用于下一页
	response := &CircleListResponse{
		Circles: documents,
		Total:   total,
		Size:    size,
	}
	if len(nextSearchAfter) > 0 && len(documents) == size {
		response.SearchAfter = nextSearchAfter
	}

	return response, nil
}

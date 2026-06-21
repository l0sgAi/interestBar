package elasticsearch

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/google/uuid"
)

// PostDocument 帖子文档结构
type PostDocument struct {
	ID            string `json:"id"`
	CircleID      string `json:"circle_id"`
	UserID        string `json:"user_id"`
	Type          int16  `json:"type"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	Content       string `json:"content"`
	ViewCount     int    `json:"view_count"`
	CommentCount  int    `json:"comment_count"`
	LikeCount     int    `json:"like_count"`
	CollectCount  int    `json:"collect_count"`
	IsPinned      int16  `json:"is_pinned"`
	IsEssence     int16  `json:"is_essence"`
	IsLock        int16  `json:"is_lock"`
	Status        int16  `json:"status"`
	Deleted       int16  `json:"deleted"`
	Hot           int    `json:"hot"`
	CreateTime    string `json:"create_time"`
	UpdateTime    string `json:"update_time"`
	LastReplyTime string `json:"last_reply_time,omitempty"`
	// 排序值（用于 search_after 分页）
	SortValues []interface{} `json:"sort_values,omitempty"`
}

// PostListResponse 帖子列表响应
type PostListResponse struct {
	Posts       []PostDocument `json:"posts"`
	Total       int64          `json:"total"`
	Size        int            `json:"size"`
	SearchAfter []interface{}  `json:"search_after,omitempty"` // 用于获取下一页
}

// SearchPosts 搜索帖子
// keyword: 搜索关键字，为空时返回所有符合条件的帖子，优先使用 title 字段检索，其次使用 summary 字段检索
// circleID: 圈子ID，为0时搜索所有圈子
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：帖子列表响应（包含帖子列表、总数、分页信息）
func SearchPosts(keyword string, circleID uuid.UUID, size int, searchAfter []interface{}) (*PostListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	// 构建搜索查询
	var searchQuery map[string]interface{}

	// 定义排序规则：按id倒序（最新的帖子id最大，在前），避免日期精度和范围问题
	sortRules := []map[string]interface{}{
		{
			"id": map[string]interface{}{
				"order": "desc",
			},
		},
	}

	// 构建基础查询条件（过滤已删除、状态正常、指定圈子）
	mustConditions := []map[string]interface{}{
		{
			"term": map[string]interface{}{
				"deleted": 0, // 过滤掉已删除的帖子
			},
		},
		{
			"term": map[string]interface{}{
				"status": 1, // 只返回已发布的帖子
			},
		},
	}

	// 如果指定了圈子ID，添加圈子过滤
	if circleID != uuid.Nil {
		mustConditions = append(mustConditions, map[string]interface{}{
			"term": map[string]interface{}{
				"circle_id": circleID.String(),
			},
		})
	}

	if keyword == "" {
		// 无关键字时，返回所有符合条件的帖子，按id倒序
		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": mustConditions,
				},
			},
			"size": size,
			"sort": sortRules,
		}
	} else {
		// 有关键字时，使用 multi_match 进行加权搜索
		// title 权重是 summary 的 3 倍，按_score排序
		sortWithScore := []map[string]interface{}{
			{
				"_score": map[string]interface{}{
					"order": "desc",
				},
			},
			{
				"id": map[string]interface{}{
					"order": "desc",
				},
			},
		}

		// 添加关键字搜索条件
		searchConditions := []map[string]interface{}{
			{
				"multi_match": map[string]interface{}{
					"query":    keyword,
					"fields":   []string{"title^3", "summary^1"},
					"type":     "best_fields",
					"operator": "or",
				},
			},
		}
		searchConditions = append(searchConditions, mustConditions...)

		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": searchConditions,
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

	// 使用帖子索引名称
	postIndex := GetPostIndexName()

	res, err := Client.Search(
		Client.Search.WithContext(nil),
		Client.Search.WithIndex(postIndex),
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

	return parsePostSearchResponse(res, size)
}

// searchUserPostsInternal 按发帖人 user_id 过滤搜索帖子，供 SearchMyPosts /
// SearchUserPosts 共用，避免重复查询体。
//
// userID: 发帖人ID（必传，核心过滤条件）
// keyword: 搜索关键字，为空时返回该用户全部帖子，优先 title 检索，其次 summary
// publishedOnly: true 时强制 status=1（仅已发布），用于查看「他人」发帖；
//
//	false 时不过滤 status（作者可见自己全部状态），用于「我的发帖」。
//
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
//
// 与 SearchPosts 的差异：
//   - 过滤条件用 user_id（而非 circle_id）；
//   - 关键字 multi_match 增加 fuzziness=AUTO，容忍拼写错误。
func searchUserPostsInternal(userID uuid.UUID, keyword string, publishedOnly bool, size int, searchAfter []interface{}) (*PostListResponse, error) {
	// 默认每页 20 条
	if size <= 0 || size > 100 {
		size = 20
	}

	// 构建搜索查询
	var searchQuery map[string]interface{}

	// 定义排序规则：按id倒序（最新的帖子id最大，在前），避免日期精度和范围问题
	sortRules := []map[string]interface{}{
		{
			"id": map[string]interface{}{
				"order": "desc",
			},
		},
	}

	// 构建基础查询条件（过滤发帖人 + 已删除）
	mustConditions := []map[string]interface{}{
		{
			"term": map[string]interface{}{
				"user_id": userID.String(), // 只返回该用户的帖子
			},
		},
		{
			"term": map[string]interface{}{
				"deleted": 0, // 过滤掉已删除的帖子
			},
		},
	}
	// 查看他人发帖时强制只返回已发布帖子（status=1）
	if publishedOnly {
		mustConditions = append(mustConditions, map[string]interface{}{
			"term": map[string]interface{}{
				"status": 1,
			},
		})
	}

	if keyword == "" {
		// 无关键字时，返回符合条件的全部帖子，按id倒序
		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": mustConditions,
				},
			},
			"size": size,
			"sort": sortRules,
		}
	} else {
		// 有关键字时，使用 multi_match 进行加权搜索
		// title 权重是 summary 的 3 倍，fuzziness=AUTO 容忍拼写错误，按_score排序
		sortWithScore := []map[string]interface{}{
			{
				"_score": map[string]interface{}{
					"order": "desc",
				},
			},
			{
				"id": map[string]interface{}{
					"order": "desc",
				},
			},
		}

		// 添加关键字搜索条件
		searchConditions := []map[string]interface{}{
			{
				"multi_match": map[string]interface{}{
					"query":     keyword,
					"fields":    []string{"title^3", "summary^1"},
					"type":      "best_fields",
					"operator":  "or",
					"fuzziness": "AUTO",
				},
			},
		}
		searchConditions = append(searchConditions, mustConditions...)

		searchQuery = map[string]interface{}{
			"query": map[string]interface{}{
				"bool": map[string]interface{}{
					"must": searchConditions,
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

	// 使用帖子索引名称
	postIndex := GetPostIndexName()

	res, err := Client.Search(
		Client.Search.WithContext(nil),
		Client.Search.WithIndex(postIndex),
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

	return parsePostSearchResponse(res, size)
}

// SearchMyPosts 搜索指定用户自己的帖子（"我的发帖"）。
// userID: 发帖人ID（必传，核心过滤条件）
// keyword: 搜索关键字，为空时返回该用户全部帖子，优先 title 检索，其次 summary
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：帖子列表响应（包含帖子列表、总数、分页信息）
//
// 不过滤 status：作者可见自己全部状态（草稿/审核/已发布/拒绝/封禁），仅排除已删除。
func SearchMyPosts(userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*PostListResponse, error) {
	return searchUserPostsInternal(userID, keyword, false, size, searchAfter)
}

// SearchUserPosts 搜索指定用户已发布的帖子（查看「他人」发帖记录用）。
// userID: 发帖人ID（必传，核心过滤条件）
// keyword: 搜索关键字，为空时返回该用户全部已发布帖子，优先 title 检索，其次 summary
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
// 返回：帖子列表响应（包含帖子列表、总数、分页信息）
//
// 与 SearchMyPosts 的差异：强制 status=1（仅已发布），他人不可见对方草稿/审核/拒绝/封禁帖。
func SearchUserPosts(userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*PostListResponse, error) {
	return searchUserPostsInternal(userID, keyword, true, size, searchAfter)
}

// parsePostSearchResponse 解析帖子搜索响应
func parsePostSearchResponse(res *esapi.Response, size int) (*PostListResponse, error) {
	var searchResult map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	hits := searchResult["hits"].(map[string]interface{})
	total := int64(hits["total"].(map[string]interface{})["value"].(float64))
	hitsList := hits["hits"].([]interface{})

	documents := make([]PostDocument, 0, len(hitsList))
	var nextSearchAfter []interface{}

	for _, hit := range hitsList {
		hitMap := hit.(map[string]interface{})
		source := hitMap["_source"]
		sourceMap := source.(map[string]interface{})

		// 获取排序值（用于下一页）
		if sortArr, ok := hitMap["sort"].([]interface{}); ok {
			if len(sortArr) > 0 {
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

		doc := PostDocument{
			ID:            getString("id"),
			CircleID:      getString("circle_id"),
			UserID:        getString("user_id"),
			Type:          getInt16("type"),
			Title:         getString("title"),
			Summary:       getString("summary"),
			Content:       getString("content"),
			ViewCount:     getInt("view_count"),
			CommentCount:  getInt("comment_count"),
			LikeCount:     getInt("like_count"),
			CollectCount:  getInt("collect_count"),
			IsPinned:      getInt16("is_pinned"),
			IsEssence:     getInt16("is_essence"),
			IsLock:        getInt16("is_lock"),
			Status:        getInt16("status"),
			Deleted:       getInt16("deleted"),
			Hot:           getInt("hot"),
			CreateTime:    getString("create_time"),
			UpdateTime:    getString("update_time"),
			LastReplyTime: getString("last_reply_time"),
		}
		documents = append(documents, doc)
	}

	// 如果有更多结果，返回 search_after 用于下一页
	response := &PostListResponse{
		Posts: documents,
		Total: total,
		Size:  size,
	}
	if len(nextSearchAfter) > 0 && len(documents) == size {
		response.SearchAfter = nextSearchAfter
	}

	return response, nil
}

// SearchCirclePosts 圈内帖子列表搜索
// circleID: 圈子ID（必传）
// sortType: 排序类型 1=近期热点 2=最新 3=精华
// size: 每页数量，默认 20
// searchAfter: 上一页返回的 search_after 值，用于获取下一页
func SearchCirclePosts(circleID uuid.UUID, sortType int, size int, searchAfter []interface{}) (*PostListResponse, error) {
	if size <= 0 || size > 100 {
		size = 20
	}

	// 共享过滤条件
	mustConditions := []map[string]interface{}{
		{"term": map[string]interface{}{"deleted": 0}},
		{"term": map[string]interface{}{"status": 1}},
		{"term": map[string]interface{}{"circle_id": circleID.String()}},
	}

	var sortRules []map[string]interface{}
	var runtimeMappings map[string]interface{}

	switch sortType {
	case 1: // 近期热点：rank_score = hot / (age_hours + 2)^0.8
		runtimeMappings = map[string]interface{}{
			"rank_score": map[string]interface{}{
				"type": "double",
				"script": map[string]interface{}{
					"source": "double ageHours = (System.currentTimeMillis() - doc['create_time'].value.toInstant().toEpochMilli()) / 3600000.0; emit(doc['hot'].value / Math.pow(ageHours + 2, 0.8));",
				},
			},
		}
		sortRules = []map[string]interface{}{
			{"rank_score": map[string]interface{}{"order": "desc"}},
			{"id": map[string]interface{}{"order": "desc"}},
		}

	case 2: // 最新：按发帖时间降序
		sortRules = []map[string]interface{}{
			{"create_time": map[string]interface{}{"order": "desc"}},
			{"id": map[string]interface{}{"order": "desc"}},
		}

	case 3: // 精华：is_essence优先，热度降序
		sortRules = []map[string]interface{}{
			{"is_essence": map[string]interface{}{"order": "desc"}},
			{"hot": map[string]interface{}{"order": "desc"}},
			{"id": map[string]interface{}{"order": "desc"}},
		}
	}

	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": mustConditions,
			},
		},
		"size": size,
		"sort": sortRules,
	}

	if runtimeMappings != nil {
		searchQuery["runtime_mappings"] = runtimeMappings
	}

	if len(searchAfter) > 0 {
		searchQuery["search_after"] = searchAfter
	}

	queryJSON, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	postIndex := GetPostIndexName()

	res, err := Client.Search(
		Client.Search.WithContext(nil),
		Client.Search.WithIndex(postIndex),
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

	return parsePostSearchResponse(res, size)
}

package elasticsearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/elastic/go-elasticsearch/v8"
)

var Client *elasticsearch.Client

// InitElasticsearch 初始化 Elasticsearch 客户端并创建索引
func InitElasticsearch() error {
	cfg := elasticsearch.Config{
		Addresses: []string{conf.Config.Elasticsearch.URL},
	}

	var err error
	Client, err = elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// 测试连接
	res, err := Client.Info()
	if err != nil {
		return fmt.Errorf("failed to get elasticsearch info: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	logger.Log.Info("Elasticsearch connected successfully")

	// 创建圈子索引
	if err := createCircleIndex(); err != nil {
		return fmt.Errorf("failed to create circle index: %w", err)
	}

	return nil
}

// createCircleIndex 创建圈子索引（如果不存在）
func createCircleIndex() error {
	indexName := GetCircleIndexName()

	// 检查索引是否已存在
	res, err := Client.Indices.Exists([]string{indexName})
	if err != nil {
		return fmt.Errorf("failed to check index existence: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == 200 {
		logger.Log.Info(fmt.Sprintf("Index '%s' already exists", indexName))
		return nil
	}

	// 定义索引映射
	mapping := map[string]interface{}{
		"settings": map[string]interface{}{
			"number_of_shards":   1,
			"number_of_replicas": 0,
			"refresh_interval":   conf.Config.Elasticsearch.RefreshInterval,
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type": "keyword",
				},
				"name": map[string]interface{}{
					"type":            "text",
					"analyzer":        "ik_max_word",
					"search_analyzer": "ik_smart",
					"fields": map[string]interface{}{
						"keyword": map[string]interface{}{
							"type":         "keyword",
							"ignore_above": 256,
						},
					},
				},
				"slug": map[string]interface{}{
					"type":         "keyword",
					"ignore_above": 256,
				},
				"avatar_url": map[string]interface{}{
					"type":         "keyword",
					"ignore_above": 500,
				},
				"description": map[string]interface{}{
					"type":            "text",
					"analyzer":        "ik_max_word",
					"search_analyzer": "ik_smart",
				},
				"hot": map[string]interface{}{
					"type": "integer",
				},
				"category_id": map[string]interface{}{
					"type": "keyword",
				},
				"member_count": map[string]interface{}{
					"type": "integer",
				},
				"post_count": map[string]interface{}{
					"type": "integer",
				},
				"create_time": map[string]interface{}{
					"type": "date",
				},
				"update_time": map[string]interface{}{
					"type": "date",
				},
				"status": map[string]interface{}{
					"type": "short",
				},
				"deleted": map[string]interface{}{
					"type": "short",
				},
				"join_type": map[string]interface{}{
					"type": "short",
				},
			},
		},
	}

	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("failed to marshal mapping: %w", err)
	}

	// 创建索引
	res, err = Client.Indices.Create(
		indexName,
		Client.Indices.Create.WithBody(bytes.NewReader(mappingJSON)),
		Client.Indices.Create.WithPretty(),
	)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("failed to create index: %s", res.String())
	}

	logger.Log.Info(fmt.Sprintf("Index '%s' created successfully", indexName))
	return nil
}

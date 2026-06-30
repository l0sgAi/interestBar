package redpanda

import (
	"context"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ItemCFSyncer item-based 协同过滤相似度定时计算器（P1）。
//
// 每 N 小时（默认 24）从 domains.post_interaction 算 post↔post 共现相似度，
// top-K 落 Redis ZSET cf:item:{post_id}（供推荐 tab C5 召回）。
// 设计见 docs/cf-item-based-design.md。
//
// 计算口径（控量）：
//   - 候选帖：近 candidate_fresh_days 天创建 + deleted=0 + status=1（防爆 + 保证可推）；
//   - 共现窗口：近 interaction_window_days 天的互动；
//   - 噪声裁剪：共现次数 < min_cooccur 的 pair 丢弃；
//   - 相似度：cosine = cooccur / √(n_i · n_j)，对称写两侧，每帖留 top-K。
type ItemCFSyncer struct {
	mu       sync.Mutex
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

var itemCFSyncer *ItemCFSyncer

// cooccurRow 共现查询行（i<j 去对称）。
type cooccurRow struct {
	I       uuid.UUID `gorm:"column:i"`
	J       uuid.UUID `gorm:"column:j"`
	Cooccur int       `gorm:"column:cooccur"`
}

// postCountRow 每帖互动者数（相似度分母）。
type postCountRow struct {
	PostID uuid.UUID `gorm:"column:post_id"`
	N      int       `gorm:"column:n"`
}

// simEntry 相似帖条目（item-based 邻居）。
type simEntry struct {
	j   uuid.UUID
	sim float64
}

// StartItemCFSyncer 启动 item-based CF 相似度计算器。
func StartItemCFSyncer() error {
	_, _, _, _, _, syncHours, _ := cfDefaults()
	s := &ItemCFSyncer{
		ticker:   time.NewTicker(time.Duration(syncHours) * time.Hour),
		stopChan: make(chan struct{}),
	}
	itemCFSyncer = s
	go s.run()
	logger.Log.Info(fmt.Sprintf("Item CF syncer started (interval=%d h)", syncHours))
	return nil
}

func (s *ItemCFSyncer) run() {
	for {
		select {
		case <-s.ticker.C:
			s.flush()
		case <-s.stopChan:
			s.ticker.Stop()
			s.flush() // 关停时排干（可选，保持与其它 syncer 一致）
			return
		}
	}
}

// flush 执行一轮：共现 → 相似度 → 写 cf:item ZSET → 清理过期互动行。
func (s *ItemCFSyncer) flush() {
	if !conf.Config.Recommend.CF.Enabled {
		return
	}
	freshDays, windowDays, minCooccur, topK, _, _, _ := cfDefaults()

	// 1. 共现 pair（i<j，去对称）
	cooccurRows, err := computeCooccurrence(freshDays, windowDays, minCooccur)
	if err != nil {
		logger.Log.Error("Item CF: failed to compute cooccurrence: " + err.Error())
		return
	}
	if len(cooccurRows) == 0 {
		logger.Log.Info("Item CF: no cooccurrence found this round")
		cleanupInteractions() // 仍清理过期行
		return
	}

	// 2. 每帖互动者数（相似度分母）
	counts, err := computePostCounts(windowDays, freshDays)
	if err != nil {
		logger.Log.Error("Item CF: failed to compute post counts: " + err.Error())
		return
	}

	// 3. 算相似度，对称累积邻居（i↔j）
	neighbors := make(map[uuid.UUID][]simEntry)
	for _, r := range cooccurRows {
		ni := counts[r.I]
		nj := counts[r.J]
		if ni == 0 || nj == 0 {
			continue
		}
		sim := float64(r.Cooccur) / math.Sqrt(float64(ni)*float64(nj))
		neighbors[r.I] = append(neighbors[r.I], simEntry{j: r.J, sim: sim})
		neighbors[r.J] = append(neighbors[r.J], simEntry{j: r.I, sim: sim})
	}

	// 4. top-K 落 Redis ZSET（分批 pipeline）
	writeAllCFItems(neighbors, topK)

	// 5. 清理过期互动行
	cleanupInteractions()
}

// computeCooccurrence 计算 candidate 帖之间的共现 pair（i<j）。
//
// 候选帖限定近 freshDays 天创建 + 已发布；共现窗口近 windowDays 天；
// 丢弃共现次数 < minCooccur 的 pair（砍单次共现噪声）。
func computeCooccurrence(freshDays, windowDays, minCooccur int) ([]cooccurRow, error) {
	sql := `
WITH candidate AS (
    SELECT id FROM domains.post
    WHERE deleted = 0 AND status = 1
      AND create_time > now() - make_interval(days => ?)
)
SELECT a.post_id AS i, b.post_id AS j, COUNT(*) AS cooccur
FROM domains.post_interaction a
JOIN domains.post_interaction b
  ON a.user_id = b.user_id AND a.post_id < b.post_id
WHERE a.ts > now() - make_interval(days => ?)
  AND b.ts > now() - make_interval(days => ?)
  AND a.post_id IN (SELECT id FROM candidate)
  AND b.post_id IN (SELECT id FROM candidate)
GROUP BY a.post_id, b.post_id
HAVING COUNT(*) >= ?
`
	var rows []cooccurRow
	if err := pgsql.DB.Raw(sql, freshDays, windowDays, windowDays, minCooccur).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// computePostCounts 计算候选帖的互动者数（相似度分母）。
func computePostCounts(windowDays, freshDays int) (map[uuid.UUID]int, error) {
	sql := `
SELECT pi.post_id, COUNT(*) AS n
FROM domains.post_interaction pi
WHERE pi.ts > now() - make_interval(days => ?)
  AND pi.post_id IN (
      SELECT id FROM domains.post
      WHERE deleted = 0 AND status = 1 AND create_time > now() - make_interval(days => ?)
  )
GROUP BY pi.post_id
`
	var rows []postCountRow
	if err := pgsql.DB.Raw(sql, windowDays, freshDays).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		m[r.PostID] = r.N
	}
	return m, nil
}

// writeAllCFItems 分批把每帖 top-K 相似帖写入 cf:item:{post_id} ZSET（DEL+ZADD+EXPIRE 覆盖式刷新）。
func writeAllCFItems(neighbors map[uuid.UUID][]simEntry, topK int) {
	if len(neighbors) == 0 {
		return
	}
	ctx := context.Background()
	ttl := time.Duration(zsetTTLHoursDefault()) * time.Hour

	posts := make([]uuid.UUID, 0, len(neighbors))
	for p := range neighbors {
		posts = append(posts, p)
	}

	const chunkSize = 200
	written := 0
	for i := 0; i < len(posts); i += chunkSize {
		end := i + chunkSize
		if end > len(posts) {
			end = len(posts)
		}
		pipe := redispkg.Client.Pipeline()
		for _, p := range posts[i:end] {
			key := redispkg.GetCFItemKey(p)
			entries := neighbors[p]
			if len(entries) > topK {
				sort.Slice(entries, func(a, b int) bool { return entries[a].sim > entries[b].sim })
				entries = entries[:topK]
			}
			members := make([]redis.Z, 0, len(entries))
			for _, e := range entries {
				members = append(members, redis.Z{Score: e.sim, Member: e.j.String()})
			}
			pipe.Del(ctx, key)
			pipe.ZAdd(ctx, key, members...)
			pipe.Expire(ctx, key, ttl)
			written++
		}
		if _, err := pipe.Exec(ctx); err != nil {
			logger.Log.Error("Item CF: failed to write cf:item batch: " + err.Error())
		}
	}
	logger.Log.Info(fmt.Sprintf("Item CF: wrote cf:item for %d posts", written))
}

// cleanupInteractions 删除超过 cleanup_days 的互动行（控表体量）。
func cleanupInteractions() {
	_, _, _, _, _, _, cleanupDays := cfDefaults()
	res := pgsql.DB.Exec(
		"DELETE FROM domains.post_interaction WHERE ts < now() - make_interval(days => ?)",
		cleanupDays,
	)
	if res.Error != nil {
		logger.Log.Error("Item CF: failed to cleanup post_interaction: " + res.Error.Error())
		return
	}
	if n := res.RowsAffected; n > 0 {
		logger.Log.Info(fmt.Sprintf("Item CF: cleaned up %d expired post_interaction rows", n))
	}
}

// cfDefaults 读取 CF 配置并给默认值（配置缺失时不阻断）。
func cfDefaults() (freshDays, windowDays, minCooccur, topK, ttlHours, syncHours, cleanupDays int) {
	cf := conf.Config.Recommend.CF
	freshDays = cf.CandidateFreshDays
	if freshDays <= 0 {
		freshDays = 30
	}
	windowDays = cf.InteractionWindowDays
	if windowDays <= 0 {
		windowDays = 90
	}
	minCooccur = cf.MinCooccur
	if minCooccur <= 0 {
		minCooccur = 2
	}
	topK = cf.TopK
	if topK <= 0 {
		topK = 50
	}
	ttlHours = cf.ZsetTTLHours
	if ttlHours <= 0 {
		ttlHours = 48
	}
	syncHours = cf.SyncIntervalHours
	if syncHours <= 0 {
		syncHours = 24
	}
	cleanupDays = cf.CleanupDays
	if cleanupDays <= 0 {
		cleanupDays = 120
	}
	return
}

// zsetTTLHoursDefault 仅返回 TTL 小数（供 writeAllCFItems 用）。
func zsetTTLHoursDefault() int {
	_, _, _, _, ttlHours, _, _ := cfDefaults()
	return ttlHours
}

// Stop 停止计算器。
func (s *ItemCFSyncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopChan)
}

// StartItemCFSyncerWithRetry 启动 item-based CF 计算器（与其他 syncer 一致签名）。
func StartItemCFSyncerWithRetry() {
	if err := StartItemCFSyncer(); err != nil {
		logger.Log.Error("Failed to start item CF syncer: " + err.Error())
	}
}

// StopItemCFSyncer 停止 item-based CF 计算器（关停时调用）。
func StopItemCFSyncer() {
	if itemCFSyncer != nil {
		itemCFSyncer.Stop()
	}
}

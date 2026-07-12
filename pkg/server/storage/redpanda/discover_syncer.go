package redpanda

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"interestBar/pkg/conf"
	discoverapp "interestBar/pkg/domains/discover/application"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"
)

// DiscoverPoolSyncer 发现页候选池定时刷新器。
//
// 每 N 分钟：① 重建匿名共享池（纯随机）；② 扫描近期活跃登录用户 token，
// 对仍在 TTL 内的用户重建其反气泡池（内容保鲜）。读路径 miss 时也会同步重建（DiscoverService 内）。
//
// 与 TrendingRankSyncer 同形：{mu, ticker, stopChan, stopped} + 优雅关停 + 排干。
// 区别：trending 是「ES 聚合 → ZSET 覆盖」；本 syncer 是「random_score 采样 → LIST 重建」，
// 重建逻辑复用 discover.application.RebuildPool（与读路径 miss 同一份代码）。
//
// 设计见 docs/discover-design.md §五。
type DiscoverPoolSyncer struct {
	mu       sync.Mutex
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
	svc      discoverapp.DiscoverService // 复用 RebuildPool（反气泡重建逻辑）
}

var discoverSyncer *DiscoverPoolSyncer

// StartDiscoverSyncer 启动发现页候选池定时同步器。svc 提供 RebuildPool 复用。
func StartDiscoverSyncer(svc discoverapp.DiscoverService) error {
	interval := conf.Config.Discover.RefreshIntervalMinutes
	if interval <= 0 {
		interval = 10
	}
	s := &DiscoverPoolSyncer{
		ticker:   time.NewTicker(time.Duration(interval) * time.Minute),
		stopChan: make(chan struct{}),
		svc:      svc,
	}
	discoverSyncer = s
	go s.run()
	logger.Log.Info(fmt.Sprintf("Discover pool syncer started (interval=%d min)", interval))
	return nil
}

func (s *DiscoverPoolSyncer) run() {
	for {
		select {
		case <-s.ticker.C:
			s.flush()
		case <-s.stopChan:
			s.ticker.Stop()
			s.flush() // 关停时排干
			return
		}
	}
}

// flush ① 重建匿名共享池 ② 扫描活跃登录用户重建其反气泡池。
func (s *DiscoverPoolSyncer) flush() {
	ctx := context.Background()

	// ① 匿名共享池（纯随机，无排除）——必重建，保证匿名落地页内容新鲜。
	if _, err := s.svc.RebuildPool(ctx, nil); err != nil {
		logger.Log.Error("discover sync rebuild anon pool failed: " + err.Error())
	}

	// ② 近期活跃登录用户池（SCAN discover:token:* 提取 TTL 内 uid）。
	activeUIDs, err := redispkg.ScanActiveDiscoverUsers(100)
	if err != nil {
		logger.Log.Error("discover sync scan active users failed: " + err.Error())
		return
	}
	for _, uidStr := range activeUIDs {
		uid, parseErr := uuid.Parse(uidStr)
		if parseErr != nil {
			continue
		}
		if _, err := s.svc.RebuildPool(ctx, &uid); err != nil {
			// 单用户重建失败不影响其它用户（best-effort）。
			logger.Log.Error("discover sync rebuild user pool failed (uid=" + uidStr + "): " + err.Error())
		}
	}
}

// Stop 停止同步器（幂等）。
func (s *DiscoverPoolSyncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopChan)
}

// StartDiscoverSyncerWithRetry 启动发现页候选池同步器（保持与其它 consumer 一致签名）。
func StartDiscoverSyncerWithRetry(svc discoverapp.DiscoverService) {
	if err := StartDiscoverSyncer(svc); err != nil {
		logger.Log.Error("Failed to start discover pool syncer: " + err.Error())
	}
}

// StopDiscoverSyncer 停止发现页候选池同步器（关停时调用）。
func StopDiscoverSyncer() {
	if discoverSyncer != nil {
		discoverSyncer.Stop()
	}
}

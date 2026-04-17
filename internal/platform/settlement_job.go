package platform

import (
	"context"
	"log/slog"
	"time"
)

// SettlementJob 每日结算定时任务（每天 01:00 UTC 执行）
type SettlementJob struct {
	svc *Service
}

func NewSettlementJob(svc *Service) *SettlementJob {
	return &SettlementJob{svc: svc}
}

// Start 启动定时任务，ctx 取消时退出
func (j *SettlementJob) Start(ctx context.Context) {
	for {
		next := nextRunTime()
		slog.Info("settlement job scheduled", "next_run", next)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			j.run(ctx)
		}
	}
}

func (j *SettlementJob) run(ctx context.Context) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	slog.Info("running daily settlement", "date", yesterday)
	// 实际汇总逻辑依赖事件数据来源，这里留扩展点
	// 生产实现：从 ClickHouse/PG 聚合 adortb.events 写入 daily_settlements
	_ = yesterday
}

// nextRunTime 计算下一次 01:00 UTC 的时间
func nextRunTime() time.Time {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

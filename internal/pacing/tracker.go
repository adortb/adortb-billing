package pacing

import (
	"sync"
	"time"
)

const trackerShards = 32

// CampaignState 保存单个广告活动的预算和消耗状态。
type CampaignState struct {
	DailyBudget float64
	Spent       float64
	UpdatedAt   time.Time
}

// Tracker 管理所有广告活动的实时消耗状态，使用分片锁降低竞争。
type Tracker struct {
	shards [trackerShards]trackerShard
	curve  *TrafficCurve
}

type trackerShard struct {
	mu     sync.RWMutex
	states map[int64]*CampaignState
	_pad   [40]byte // 填充到 64 字节，避免 false sharing
}

// NewTracker 创建 Tracker，使用 DefaultCurve。
func NewTracker(curve *TrafficCurve) *Tracker {
	if curve == nil {
		curve = DefaultCurve()
	}
	t := &Tracker{curve: curve}
	for i := range t.shards {
		t.shards[i].states = make(map[int64]*CampaignState)
	}
	return t
}

// SetBudget 设置广告活动的日预算（新增或更新）。
func (t *Tracker) SetBudget(campaignID int64, dailyBudget float64) {
	s := t.shard(campaignID)
	s.mu.Lock()
	st, ok := s.states[campaignID]
	if !ok {
		st = &CampaignState{}
		s.states[campaignID] = st
	}
	st.DailyBudget = dailyBudget
	st.UpdatedAt = time.Now()
	s.mu.Unlock()
}

// SetSpent 直接设置已消耗金额（来自账单系统的全量同步）。
func (t *Tracker) SetSpent(campaignID int64, spent float64) {
	s := t.shard(campaignID)
	s.mu.Lock()
	st, ok := s.states[campaignID]
	if !ok {
		st = &CampaignState{}
		s.states[campaignID] = st
	}
	if spent >= 0 {
		st.Spent = spent
	}
	st.UpdatedAt = time.Now()
	s.mu.Unlock()
}

// AddSpend 原子累加消耗金额（实时竞价扣费）。
func (t *Tracker) AddSpend(campaignID int64, amount float64) {
	if amount <= 0 {
		return
	}
	s := t.shard(campaignID)
	s.mu.Lock()
	st, ok := s.states[campaignID]
	if ok {
		st.Spent += amount
		st.UpdatedAt = time.Now()
	}
	s.mu.Unlock()
}

// Factor 返回广告活动当前时刻的 pacing factor。
// 广告活动不存在时返回 1.0（不干预）。
func (t *Tracker) Factor(campaignID int64) float64 {
	return t.FactorAt(campaignID, time.Now())
}

// FactorAt 返回指定时刻的 pacing factor（便于测试）。
func (t *Tracker) FactorAt(campaignID int64, now time.Time) float64 {
	s := t.shard(campaignID)
	s.mu.RLock()
	st, ok := s.states[campaignID]
	var daily, spent float64
	if ok {
		daily = st.DailyBudget
		spent = st.Spent
	}
	s.mu.RUnlock()
	if !ok || daily <= 0 {
		return 1.0
	}
	return Factor(daily, spent, t.curve, now)
}

// State 返回广告活动状态快照（只读副本）。
func (t *Tracker) State(campaignID int64) (CampaignState, bool) {
	s := t.shard(campaignID)
	s.mu.RLock()
	st, ok := s.states[campaignID]
	if !ok {
		s.mu.RUnlock()
		return CampaignState{}, false
	}
	copy := *st
	s.mu.RUnlock()
	return copy, true
}

// Recalc 强制重新计算所有 campaign 的 pacing（触发外部流量曲线学习）。
// 当前实现为 no-op placeholder，预留给未来批量刷新逻辑。
func (t *Tracker) Recalc() {}

func (t *Tracker) shard(id int64) *trackerShard {
	// Fibonacci hashing for int64
	idx := (uint64(id) * 11400714819323198485) >> (64 - 5)
	return &t.shards[idx]
}

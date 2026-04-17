package advertiser_billing

import (
	"context"
	"fmt"

	"github.com/adortb/adortb-billing/internal/account"
	"github.com/adortb/adortb-billing/internal/metrics"
	"github.com/adortb/adortb-billing/internal/repo"
	goredis "github.com/redis/go-redis/v9"
)

// Service 广告主计费服务：充值 + 余额查询 + 事件扣费
type Service struct {
	advRepo repo.AdvertiserRepo
	redis   *goredis.Client
	metrics *metrics.Metrics
}

func NewService(advRepo repo.AdvertiserRepo, redis *goredis.Client, m *metrics.Metrics) *Service {
	return &Service{advRepo: advRepo, redis: redis, metrics: m}
}

func (s *Service) Recharge(ctx context.Context, advertiserID int64, amount float64) (*account.BalanceTransaction, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	tx, err := s.advRepo.Recharge(ctx, advertiserID, amount)
	if err != nil {
		return nil, err
	}
	// 同步更新 Redis 缓存余额
	_ = s.syncRedisBalance(ctx, advertiserID, tx.BalanceAfter)
	s.metrics.RecordRecharge(advertiserID, amount)
	return tx, nil
}

func (s *Service) GetAccount(ctx context.Context, advertiserID int64) (*account.AdvertiserAccount, error) {
	return s.advRepo.GetOrCreate(ctx, advertiserID)
}

func (s *Service) ListTransactions(ctx context.Context, advertiserID int64, limit int) ([]*account.BalanceTransaction, error) {
	return s.advRepo.ListTransactions(ctx, advertiserID, limit)
}

// SpendForEvent 通过 Redis Lua 原子扣费，PG 流水由 consumer 批量写入
// 若 Redis 中无余额缓存则回落到 PG 行锁扣费
func (s *Service) SpendForEvent(ctx context.Context, advertiserID int64, amount float64, eventID string) error {
	if amount <= 0 {
		return nil
	}

	spent, err := s.tryRedisSpend(ctx, advertiserID, amount)
	if err != nil || !spent {
		// 回落 PG
		_, pgErr := s.advRepo.SpendAtomic(ctx, advertiserID, amount,
			string(account.RefTypeEvent), eventID, "impression 扣费")
		if pgErr != nil {
			return pgErr
		}
	}
	s.metrics.RecordSpend(advertiserID, amount)
	return nil
}

// SpendAtomicPG 直接 PG 行锁扣费（对账修正用）
func (s *Service) SpendAtomicPG(ctx context.Context, advertiserID int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error) {
	return s.advRepo.SpendAtomic(ctx, advertiserID, amount, refType, refID, desc)
}

// redisBalanceKey 缓存 key
func redisBalanceKey(advertiserID int64) string {
	return fmt.Sprintf("advertiser:balance:%d", advertiserID)
}

// luaSpend 原子扣费脚本：KEYS[1]=balance key; ARGV[1]=amount
// 返回 1 成功，0 余额不足，-1 key 不存在
var luaSpend = goredis.NewScript(`
local bal = redis.call('HGET', KEYS[1], 'balance')
if bal == false then return -1 end
local b = tonumber(bal)
local a = tonumber(ARGV[1])
if b < a then return 0 end
local newbal = b - a
redis.call('HSET', KEYS[1], 'balance', tostring(newbal))
return 1
`)

func (s *Service) tryRedisSpend(ctx context.Context, advertiserID int64, amount float64) (bool, error) {
	if s.redis == nil {
		return false, nil
	}
	key := redisBalanceKey(advertiserID)
	res, err := luaSpend.Run(ctx, s.redis, []string{key}, amount).Int()
	if err != nil {
		return false, err
	}
	switch res {
	case 1:
		return true, nil
	case 0:
		return false, repo.ErrInsufficientBalance
	default:
		return false, nil // key 不存在，回落 PG
	}
}

func (s *Service) syncRedisBalance(ctx context.Context, advertiserID int64, balance float64) error {
	if s.redis == nil {
		return nil
	}
	key := redisBalanceKey(advertiserID)
	return s.redis.HSet(ctx, key, "balance", balance).Err()
}

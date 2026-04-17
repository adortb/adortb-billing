package platform

import (
	"context"
	"math"

	"github.com/adortb/adortb-billing/internal/account"
	"github.com/adortb/adortb-billing/internal/repo"
)

// Service 平台侧：抽成计算 + 每日汇总
type Service struct {
	platRepo repo.PlatformRepo
}

func NewService(platRepo repo.PlatformRepo) *Service {
	return &Service{platRepo: platRepo}
}

// CalcFees 计算平台抽成 + 媒体方净收入
func (s *Service) CalcFees(ctx context.Context, grossRevenue float64) (platformFee, netRevenue float64, err error) {
	rate, err := s.platRepo.GetFeeRate(ctx)
	if err != nil {
		return 0, 0, err
	}
	platformFee = round4(grossRevenue * rate)
	netRevenue = round4(grossRevenue - platformFee)
	return platformFee, netRevenue, nil
}

func (s *Service) GetDailySummary(ctx context.Context, date string) ([]*account.DailySettlement, error) {
	return s.platRepo.GetDailySummary(ctx, date)
}

func (s *Service) UpsertDailySettlement(ctx context.Context, settlement *account.DailySettlement) error {
	return s.platRepo.UpsertDailySettlement(ctx, settlement)
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

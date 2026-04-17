package publisher_billing

import (
	"context"

	"github.com/adortb/adortb-billing/internal/account"
	"github.com/adortb/adortb-billing/internal/metrics"
	"github.com/adortb/adortb-billing/internal/repo"
)

// Service 媒体方计费：收入累计、结算单查询、提现申请、审批
type Service struct {
	pubRepo repo.PublisherRepo
	metrics *metrics.Metrics
}

func NewService(pubRepo repo.PublisherRepo, m *metrics.Metrics) *Service {
	return &Service{pubRepo: pubRepo, metrics: m}
}

func (s *Service) GetAccount(ctx context.Context, publisherID int64) (*account.PublisherAccount, error) {
	return s.pubRepo.GetOrCreate(ctx, publisherID)
}

func (s *Service) AddRevenue(ctx context.Context, publisherID int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error) {
	tx, err := s.pubRepo.AddRevenue(ctx, publisherID, amount, refType, refID, desc)
	if err != nil {
		return nil, err
	}
	s.metrics.RecordPublisherRevenue(publisherID, amount)
	return tx, nil
}

func (s *Service) ListSettlements(ctx context.Context, publisherID int64, from, to string) ([]*account.DailySettlement, error) {
	return s.pubRepo.ListSettlements(ctx, publisherID, from, to)
}

func (s *Service) CreateWithdraw(ctx context.Context, publisherID int64, amount float64, bankInfo []byte) (*account.WithdrawRequest, error) {
	return s.pubRepo.CreateWithdrawRequest(ctx, publisherID, amount, bankInfo)
}

func (s *Service) ApproveWithdraw(ctx context.Context, withdrawID, reviewerID int64) error {
	return s.pubRepo.ApproveWithdraw(ctx, withdrawID, reviewerID)
}

func (s *Service) GetWithdrawRequest(ctx context.Context, withdrawID int64) (*account.WithdrawRequest, error) {
	return s.pubRepo.GetWithdrawRequest(ctx, withdrawID)
}

// SettlePending 每日结算：将 pending 转为 settled
func (s *Service) SettlePending(ctx context.Context, publisherID int64, amount float64) error {
	return s.pubRepo.SettlePending(ctx, publisherID, amount)
}

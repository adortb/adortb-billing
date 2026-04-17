package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/adortb/adortb-billing/internal/account"
)

type PlatformRepo interface {
	GetFeeRate(ctx context.Context) (float64, error)
	GetDailySummary(ctx context.Context, date string) ([]*account.DailySettlement, error)
	UpsertDailySettlement(ctx context.Context, s *account.DailySettlement) error
}

type pgPlatformRepo struct {
	db *sql.DB
}

func NewPlatformRepo(db *sql.DB) PlatformRepo {
	return &pgPlatformRepo{db: db}
}

func (r *pgPlatformRepo) GetFeeRate(ctx context.Context) (float64, error) {
	var val string
	if err := r.db.QueryRowContext(ctx,
		`SELECT value FROM platform_configs WHERE key = 'platform_fee_rate'`,
	).Scan(&val); err != nil {
		if err == sql.ErrNoRows {
			return 0.10, nil // 默认 10%
		}
		return 0, fmt.Errorf("get fee rate: %w", err)
	}
	rate, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, fmt.Errorf("parse fee rate: %w", err)
	}
	return rate, nil
}

func (r *pgPlatformRepo) GetDailySummary(ctx context.Context, date string) ([]*account.DailySettlement, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, date::TEXT, advertiser_id, publisher_id, campaign_id,
		        impressions, clicks, gross_revenue, platform_fee, net_revenue
		 FROM daily_settlements WHERE date = $1::date ORDER BY id`,
		date,
	)
	if err != nil {
		return nil, fmt.Errorf("query daily summary: %w", err)
	}
	defer rows.Close()
	return scanSettlements(rows)
}

func (r *pgPlatformRepo) UpsertDailySettlement(ctx context.Context, s *account.DailySettlement) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO daily_settlements
		 (date, advertiser_id, publisher_id, campaign_id, impressions, clicks, gross_revenue, platform_fee, net_revenue)
		 VALUES ($1::date,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (date, advertiser_id, publisher_id, campaign_id) DO UPDATE
		   SET impressions = EXCLUDED.impressions,
		       clicks = EXCLUDED.clicks,
		       gross_revenue = EXCLUDED.gross_revenue,
		       platform_fee = EXCLUDED.platform_fee,
		       net_revenue = EXCLUDED.net_revenue`,
		s.Date, s.AdvertiserID, s.PublisherID, s.CampaignID,
		s.Impressions, s.Clicks, s.GrossRevenue, s.PlatformFee, s.NetRevenue,
	)
	return err
}

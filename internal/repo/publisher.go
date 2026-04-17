package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/adortb/adortb-billing/internal/account"
)

type PublisherRepo interface {
	GetOrCreate(ctx context.Context, publisherID int64) (*account.PublisherAccount, error)
	Get(ctx context.Context, publisherID int64) (*account.PublisherAccount, error)
	AddRevenue(ctx context.Context, publisherID int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error)
	ListSettlements(ctx context.Context, publisherID int64, from, to string) ([]*account.DailySettlement, error)
	CreateWithdrawRequest(ctx context.Context, publisherID int64, amount float64, bankInfo []byte) (*account.WithdrawRequest, error)
	ApproveWithdraw(ctx context.Context, withdrawID, reviewerID int64) error
	GetWithdrawRequest(ctx context.Context, withdrawID int64) (*account.WithdrawRequest, error)
	// SettlePending 将 revenue_pending 转移到 revenue_settled（每日结算调用）
	SettlePending(ctx context.Context, publisherID int64, amount float64) error
}

type pgPublisherRepo struct {
	db *sql.DB
}

func NewPublisherRepo(db *sql.DB) PublisherRepo {
	return &pgPublisherRepo{db: db}
}

func (r *pgPublisherRepo) GetOrCreate(ctx context.Context, publisherID int64) (*account.PublisherAccount, error) {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO publisher_accounts (publisher_id) VALUES ($1) ON CONFLICT DO NOTHING`,
		publisherID,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert publisher account: %w", err)
	}
	return r.Get(ctx, publisherID)
}

func (r *pgPublisherRepo) Get(ctx context.Context, publisherID int64) (*account.PublisherAccount, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT publisher_id, revenue_pending, revenue_settled, revenue_withdrawn, created_at, updated_at
		 FROM publisher_accounts WHERE publisher_id = $1`,
		publisherID,
	)
	a := &account.PublisherAccount{}
	if err := row.Scan(&a.PublisherID, &a.RevenuePending, &a.RevenueSettled, &a.RevenueWithdrawn, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("scan publisher account: %w", err)
	}
	return a, nil
}

func (r *pgPublisherRepo) AddRevenue(ctx context.Context, publisherID int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("revenue amount must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var pendingAfter float64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO publisher_accounts (publisher_id, revenue_pending)
		 VALUES ($1, $2)
		 ON CONFLICT (publisher_id) DO UPDATE
		   SET revenue_pending = publisher_accounts.revenue_pending + $2,
		       updated_at = NOW()
		 RETURNING revenue_pending`,
		publisherID, amount,
	).Scan(&pendingAfter); err != nil {
		return nil, fmt.Errorf("add revenue update: %w", err)
	}

	btx := &account.BalanceTransaction{
		AccountType:  account.AccountTypePublisher,
		AccountID:    publisherID,
		TxType:       account.TxTypeRevenue,
		Amount:       amount,
		BalanceAfter: pendingAfter,
		RefType:      refType,
		RefID:        refID,
		Description:  desc,
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO balance_transactions
		 (account_type, account_id, tx_type, amount, balance_after, ref_type, ref_id, description)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, created_at`,
		btx.AccountType, btx.AccountID, btx.TxType, btx.Amount, btx.BalanceAfter,
		btx.RefType, btx.RefID, btx.Description,
	).Scan(&btx.ID, &btx.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert revenue tx: %w", err)
	}

	return btx, tx.Commit()
}

func (r *pgPublisherRepo) ListSettlements(ctx context.Context, publisherID int64, from, to string) ([]*account.DailySettlement, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, date::TEXT, advertiser_id, publisher_id, campaign_id,
		        impressions, clicks, gross_revenue, platform_fee, net_revenue
		 FROM daily_settlements
		 WHERE publisher_id = $1 AND date BETWEEN $2::date AND $3::date
		 ORDER BY date DESC`,
		publisherID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("query settlements: %w", err)
	}
	defer rows.Close()
	return scanSettlements(rows)
}

func scanSettlements(rows *sql.Rows) ([]*account.DailySettlement, error) {
	var list []*account.DailySettlement
	for rows.Next() {
		s := &account.DailySettlement{}
		if err := rows.Scan(&s.ID, &s.Date, &s.AdvertiserID, &s.PublisherID, &s.CampaignID,
			&s.Impressions, &s.Clicks, &s.GrossRevenue, &s.PlatformFee, &s.NetRevenue); err != nil {
			return nil, fmt.Errorf("scan settlement: %w", err)
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

func (r *pgPublisherRepo) CreateWithdrawRequest(ctx context.Context, publisherID int64, amount float64, bankInfo []byte) (*account.WithdrawRequest, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("withdraw amount must be positive")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var settled float64
	if err := tx.QueryRowContext(ctx,
		`SELECT revenue_settled FROM publisher_accounts WHERE publisher_id = $1 FOR UPDATE`,
		publisherID,
	).Scan(&settled); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("lock publisher account: %w", err)
	}
	if settled < amount {
		return nil, ErrInsufficientBalance
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE publisher_accounts
		 SET revenue_settled = revenue_settled - $2,
		     revenue_withdrawn = revenue_withdrawn + $2,
		     updated_at = NOW()
		 WHERE publisher_id = $1`,
		publisherID, amount,
	); err != nil {
		return nil, fmt.Errorf("update publisher for withdraw: %w", err)
	}

	wr := &account.WithdrawRequest{
		PublisherID: publisherID,
		Amount:      amount,
		Status:      account.WithdrawStatusPending,
		BankInfo:    bankInfo,
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO withdraw_requests (publisher_id, amount, bank_info)
		 VALUES ($1,$2,$3) RETURNING id, created_at`,
		wr.PublisherID, wr.Amount, wr.BankInfo,
	).Scan(&wr.ID, &wr.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert withdraw request: %w", err)
	}

	return wr, tx.Commit()
}

func (r *pgPublisherRepo) GetWithdrawRequest(ctx context.Context, withdrawID int64) (*account.WithdrawRequest, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, publisher_id, amount, status, bank_info, reviewed_by, reviewed_at, created_at
		 FROM withdraw_requests WHERE id = $1`,
		withdrawID,
	)
	wr := &account.WithdrawRequest{}
	var reviewedAt sql.NullTime
	var reviewedBy sql.NullInt64
	if err := row.Scan(&wr.ID, &wr.PublisherID, &wr.Amount, &wr.Status, &wr.BankInfo,
		&reviewedBy, &reviewedAt, &wr.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("withdraw request not found")
		}
		return nil, fmt.Errorf("scan withdraw request: %w", err)
	}
	if reviewedBy.Valid {
		v := reviewedBy.Int64
		wr.ReviewedBy = &v
	}
	if reviewedAt.Valid {
		v := reviewedAt.Time
		wr.ReviewedAt = &v
	}
	return wr, nil
}

func (r *pgPublisherRepo) ApproveWithdraw(ctx context.Context, withdrawID, reviewerID int64) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`UPDATE withdraw_requests
		 SET status = $1, reviewed_by = $2, reviewed_at = $3
		 WHERE id = $4 AND status = 'pending'`,
		account.WithdrawStatusApproved, reviewerID, now, withdrawID,
	)
	if err != nil {
		return fmt.Errorf("approve withdraw: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("withdraw request not found or already processed")
	}
	return nil
}

func (r *pgPublisherRepo) SettlePending(ctx context.Context, publisherID int64, amount float64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE publisher_accounts
		 SET revenue_pending = revenue_pending - $2,
		     revenue_settled = revenue_settled + $2,
		     updated_at = NOW()
		 WHERE publisher_id = $1`,
		publisherID, amount,
	)
	return err
}

package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/adortb/adortb-billing/internal/account"
)

type AdvertiserRepo interface {
	GetOrCreate(ctx context.Context, advertiserID int64) (*account.AdvertiserAccount, error)
	Get(ctx context.Context, advertiserID int64) (*account.AdvertiserAccount, error)
	Recharge(ctx context.Context, advertiserID int64, amount float64) (*account.BalanceTransaction, error)
	// SpendAtomic 使用行锁在 PG 中原子扣费（余额不足返回 ErrInsufficientBalance）
	SpendAtomic(ctx context.Context, advertiserID int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error)
	ListTransactions(ctx context.Context, advertiserID int64, limit int) ([]*account.BalanceTransaction, error)
}

type pgAdvertiserRepo struct {
	db *sql.DB
}

func NewAdvertiserRepo(db *sql.DB) AdvertiserRepo {
	return &pgAdvertiserRepo{db: db}
}

var ErrInsufficientBalance = fmt.Errorf("insufficient balance")
var ErrAccountNotFound = fmt.Errorf("account not found")

func (r *pgAdvertiserRepo) GetOrCreate(ctx context.Context, advertiserID int64) (*account.AdvertiserAccount, error) {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO advertiser_accounts (advertiser_id) VALUES ($1) ON CONFLICT DO NOTHING`,
		advertiserID,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert advertiser account: %w", err)
	}
	return r.Get(ctx, advertiserID)
}

func (r *pgAdvertiserRepo) Get(ctx context.Context, advertiserID int64) (*account.AdvertiserAccount, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT advertiser_id, balance, frozen, total_recharge, total_spent, created_at, updated_at
		 FROM advertiser_accounts WHERE advertiser_id = $1`,
		advertiserID,
	)
	a := &account.AdvertiserAccount{}
	if err := row.Scan(&a.AdvertiserID, &a.Balance, &a.Frozen, &a.TotalRecharge, &a.TotalSpent, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("scan advertiser account: %w", err)
	}
	return a, nil
}

func (r *pgAdvertiserRepo) Recharge(ctx context.Context, advertiserID int64, amount float64) (*account.BalanceTransaction, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("recharge amount must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	row := tx.QueryRowContext(ctx,
		`INSERT INTO advertiser_accounts (advertiser_id, balance, total_recharge)
		 VALUES ($1, $2, $2)
		 ON CONFLICT (advertiser_id) DO UPDATE
		   SET balance = advertiser_accounts.balance + $2,
		       total_recharge = advertiser_accounts.total_recharge + $2,
		       updated_at = NOW()
		 RETURNING balance`,
		advertiserID, amount,
	)
	var balanceAfter float64
	if err := row.Scan(&balanceAfter); err != nil {
		return nil, fmt.Errorf("recharge update: %w", err)
	}

	txRow := tx.QueryRowContext(ctx,
		`INSERT INTO balance_transactions
		 (account_type, account_id, tx_type, amount, balance_after, ref_type, description)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id, created_at`,
		account.AccountTypeAdvertiser, advertiserID, account.TxTypeRecharge, amount, balanceAfter,
		"recharge", "广告主充值",
	)
	btx := &account.BalanceTransaction{
		AccountType:  account.AccountTypeAdvertiser,
		AccountID:    advertiserID,
		TxType:       account.TxTypeRecharge,
		Amount:       amount,
		BalanceAfter: balanceAfter,
	}
	if err := txRow.Scan(&btx.ID, &btx.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert balance tx: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit recharge: %w", err)
	}
	return btx, nil
}

func (r *pgAdvertiserRepo) SpendAtomic(ctx context.Context, advertiserID int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("spend amount must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var balance float64
	if err := tx.QueryRowContext(ctx,
		`SELECT balance FROM advertiser_accounts WHERE advertiser_id = $1 FOR UPDATE`,
		advertiserID,
	).Scan(&balance); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("lock advertiser account: %w", err)
	}

	if balance < amount {
		return nil, ErrInsufficientBalance
	}

	var balanceAfter float64
	if err := tx.QueryRowContext(ctx,
		`UPDATE advertiser_accounts
		 SET balance = balance - $2, total_spent = total_spent + $2, updated_at = NOW()
		 WHERE advertiser_id = $1
		 RETURNING balance`,
		advertiserID, amount,
	).Scan(&balanceAfter); err != nil {
		return nil, fmt.Errorf("spend update: %w", err)
	}

	btx := &account.BalanceTransaction{
		AccountType:  account.AccountTypeAdvertiser,
		AccountID:    advertiserID,
		TxType:       account.TxTypeSpend,
		Amount:       -amount,
		BalanceAfter: balanceAfter,
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
		return nil, fmt.Errorf("insert spend tx: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit spend: %w", err)
	}
	return btx, nil
}

func (r *pgAdvertiserRepo) ListTransactions(ctx context.Context, advertiserID int64, limit int) ([]*account.BalanceTransaction, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, account_type, account_id, tx_type, amount, balance_after,
		        COALESCE(ref_type,''), COALESCE(ref_id,''), COALESCE(description,''), created_at
		 FROM balance_transactions
		 WHERE account_type = $1 AND account_id = $2
		 ORDER BY created_at DESC LIMIT $3`,
		account.AccountTypeAdvertiser, advertiserID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()
	return scanTransactions(rows)
}

func scanTransactions(rows *sql.Rows) ([]*account.BalanceTransaction, error) {
	var txs []*account.BalanceTransaction
	for rows.Next() {
		t := &account.BalanceTransaction{}
		var createdAt time.Time
		if err := rows.Scan(&t.ID, &t.AccountType, &t.AccountID, &t.TxType,
			&t.Amount, &t.BalanceAfter, &t.RefType, &t.RefID, &t.Description, &createdAt); err != nil {
			return nil, fmt.Errorf("scan tx: %w", err)
		}
		t.CreatedAt = createdAt
		txs = append(txs, t)
	}
	return txs, rows.Err()
}

package repo

import (
	"context"
	"fmt"
	"sync"

	"github.com/adortb/adortb-billing/internal/account"
)

// MockAdvertiserRepo 内存实现，用于测试
type MockAdvertiserRepo struct {
	mu       sync.Mutex
	accounts map[int64]*account.AdvertiserAccount
	txs      []*account.BalanceTransaction
	nextTxID int64
}

func NewMockAdvertiserRepo() *MockAdvertiserRepo {
	return &MockAdvertiserRepo{
		accounts: make(map[int64]*account.AdvertiserAccount),
		nextTxID: 1,
	}
}

func (m *MockAdvertiserRepo) GetOrCreate(ctx context.Context, id int64) (*account.AdvertiserAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		m.accounts[id] = &account.AdvertiserAccount{AdvertiserID: id}
	}
	cp := *m.accounts[id]
	return &cp, nil
}

func (m *MockAdvertiserRepo) Get(ctx context.Context, id int64) (*account.AdvertiserAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *MockAdvertiserRepo) Recharge(ctx context.Context, id int64, amount float64) (*account.BalanceTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		m.accounts[id] = &account.AdvertiserAccount{AdvertiserID: id}
	}
	m.accounts[id].Balance += amount
	m.accounts[id].TotalRecharge += amount
	tx := m.newTx(account.AccountTypeAdvertiser, id, account.TxTypeRecharge, amount, m.accounts[id].Balance)
	return tx, nil
}

func (m *MockAdvertiserRepo) SpendAtomic(ctx context.Context, id int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	if a.Balance < amount {
		return nil, ErrInsufficientBalance
	}
	a.Balance -= amount
	a.TotalSpent += amount
	tx := m.newTx(account.AccountTypeAdvertiser, id, account.TxTypeSpend, -amount, a.Balance)
	tx.RefType = refType
	tx.RefID = refID
	tx.Description = desc
	return tx, nil
}

func (m *MockAdvertiserRepo) ListTransactions(ctx context.Context, id int64, limit int) ([]*account.BalanceTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*account.BalanceTransaction
	for _, tx := range m.txs {
		if tx.AccountID == id {
			result = append(result, tx)
		}
	}
	if len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result, nil
}

func (m *MockAdvertiserRepo) newTx(atype account.AccountType, id int64, txType account.TxType, amount, balAfter float64) *account.BalanceTransaction {
	tx := &account.BalanceTransaction{
		ID:           m.nextTxID,
		AccountType:  atype,
		AccountID:    id,
		TxType:       txType,
		Amount:       amount,
		BalanceAfter: balAfter,
	}
	m.nextTxID++
	m.txs = append(m.txs, tx)
	return tx
}

// MockPublisherRepo 内存实现
type MockPublisherRepo struct {
	mu          sync.Mutex
	accounts    map[int64]*account.PublisherAccount
	settlements []*account.DailySettlement
	withdraws   map[int64]*account.WithdrawRequest
	nextID      int64
}

func NewMockPublisherRepo() *MockPublisherRepo {
	return &MockPublisherRepo{
		accounts:  make(map[int64]*account.PublisherAccount),
		withdraws: make(map[int64]*account.WithdrawRequest),
		nextID:    1,
	}
}

func (m *MockPublisherRepo) GetOrCreate(ctx context.Context, id int64) (*account.PublisherAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		m.accounts[id] = &account.PublisherAccount{PublisherID: id}
	}
	cp := *m.accounts[id]
	return &cp, nil
}

func (m *MockPublisherRepo) Get(ctx context.Context, id int64) (*account.PublisherAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *MockPublisherRepo) AddRevenue(ctx context.Context, id int64, amount float64, refType, refID, desc string) (*account.BalanceTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[id]; !ok {
		m.accounts[id] = &account.PublisherAccount{PublisherID: id}
	}
	m.accounts[id].RevenuePending += amount
	return &account.BalanceTransaction{
		ID:           m.nextID,
		AccountType:  account.AccountTypePublisher,
		AccountID:    id,
		TxType:       account.TxTypeRevenue,
		Amount:       amount,
		BalanceAfter: m.accounts[id].RevenuePending,
	}, nil
}

func (m *MockPublisherRepo) ListSettlements(ctx context.Context, id int64, from, to string) ([]*account.DailySettlement, error) {
	return m.settlements, nil
}

func (m *MockPublisherRepo) CreateWithdrawRequest(ctx context.Context, id int64, amount float64, bankInfo []byte) (*account.WithdrawRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	if a.RevenueSettled < amount {
		return nil, ErrInsufficientBalance
	}
	a.RevenueSettled -= amount
	a.RevenueWithdrawn += amount
	wr := &account.WithdrawRequest{
		ID:          m.nextID,
		PublisherID: id,
		Amount:      amount,
		Status:      account.WithdrawStatusPending,
		BankInfo:    bankInfo,
	}
	m.nextID++
	m.withdraws[wr.ID] = wr
	return wr, nil
}

func (m *MockPublisherRepo) GetWithdrawRequest(ctx context.Context, id int64) (*account.WithdrawRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wr, ok := m.withdraws[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *wr
	return &cp, nil
}

func (m *MockPublisherRepo) ApproveWithdraw(ctx context.Context, wid, reviewerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	wr, ok := m.withdraws[wid]
	if !ok {
		return fmt.Errorf("not found")
	}
	wr.Status = account.WithdrawStatusApproved
	wr.ReviewedBy = &reviewerID
	return nil
}

// InjectSettled 测试辅助：直接设置已结算余额
func (m *MockPublisherRepo) InjectSettled(publisherID int64, amount float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.accounts[publisherID]; !ok {
		m.accounts[publisherID] = &account.PublisherAccount{PublisherID: publisherID}
	}
	m.accounts[publisherID].RevenueSettled = amount
}

func (m *MockPublisherRepo) SettlePending(ctx context.Context, id int64, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return ErrAccountNotFound
	}
	a.RevenuePending -= amount
	a.RevenueSettled += amount
	return nil
}

// MockPlatformRepo
type MockPlatformRepo struct {
	FeeRate     float64
	settlements []*account.DailySettlement
}

func NewMockPlatformRepo(rate float64) *MockPlatformRepo {
	return &MockPlatformRepo{FeeRate: rate}
}

func (m *MockPlatformRepo) GetFeeRate(_ context.Context) (float64, error) {
	return m.FeeRate, nil
}

func (m *MockPlatformRepo) GetDailySummary(_ context.Context, date string) ([]*account.DailySettlement, error) {
	return m.settlements, nil
}

func (m *MockPlatformRepo) UpsertDailySettlement(_ context.Context, s *account.DailySettlement) error {
	m.settlements = append(m.settlements, s)
	return nil
}

package advertiser_billing_test

import (
	"context"
	"sync"
	"testing"

	"github.com/adortb/adortb-billing/internal/advertiser_billing"
	"github.com/adortb/adortb-billing/internal/metrics"
	"github.com/adortb/adortb-billing/internal/repo"
	"github.com/prometheus/client_golang/prometheus"
)

func newTestService() *advertiser_billing.Service {
	mockRepo := repo.NewMockAdvertiserRepo()
	m := metrics.New(prometheus.NewRegistry())
	return advertiser_billing.NewService(mockRepo, nil, m)
}

func TestRecharge_Basic(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	tx, err := svc.Recharge(ctx, 1001, 100.0)
	if err != nil {
		t.Fatalf("recharge error: %v", err)
	}
	if tx.Amount != 100.0 {
		t.Errorf("expected amount 100.0, got %v", tx.Amount)
	}
	if tx.BalanceAfter != 100.0 {
		t.Errorf("expected balance_after 100.0, got %v", tx.BalanceAfter)
	}
}

func TestRecharge_InvalidAmount(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.Recharge(ctx, 1001, -10.0)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestGetAccount_AutoCreate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	acc, err := svc.GetAccount(ctx, 9999)
	if err != nil {
		t.Fatalf("get account error: %v", err)
	}
	if acc.AdvertiserID != 9999 {
		t.Errorf("expected advertiser_id 9999, got %v", acc.AdvertiserID)
	}
}

func TestSpendForEvent_InsufficientBalance(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// 充值 10，尝试扣费 50
	if _, err := svc.Recharge(ctx, 2001, 10.0); err != nil {
		t.Fatal(err)
	}
	err := svc.SpendForEvent(ctx, 2001, 50.0, "evt-001")
	if err == nil {
		t.Fatal("expected insufficient balance error")
	}
}

func TestSpendForEvent_Sufficient(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Recharge(ctx, 3001, 100.0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpendForEvent(ctx, 3001, 1.5, "evt-002"); err != nil {
		t.Fatalf("spend error: %v", err)
	}

	acc, _ := svc.GetAccount(ctx, 3001)
	if acc.Balance != 98.5 {
		t.Errorf("expected balance 98.5, got %v", acc.Balance)
	}
}

// TestConcurrentSpend 并发扣费不能产生负余额
func TestConcurrentSpend(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	const advertiserID = 4001
	if _, err := svc.Recharge(ctx, advertiserID, 50.0); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := svc.SpendForEvent(ctx, advertiserID, 1.0, "evt-concurrent")
			if err != nil {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	acc, _ := svc.GetAccount(ctx, advertiserID)
	if acc.Balance < 0 {
		t.Errorf("balance went negative: %v", acc.Balance)
	}
	// 成功扣费次数 = 50 次，失败 50 次
	if errCount != 50 {
		t.Errorf("expected 50 errors, got %d (balance=%v)", errCount, acc.Balance)
	}
}

func TestListTransactions(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Recharge(ctx, 5001, 100.0); err != nil {
		t.Fatal(err)
	}
	_ = svc.SpendForEvent(ctx, 5001, 10.0, "evt-003")

	txs, err := svc.ListTransactions(ctx, 5001, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) < 1 {
		t.Error("expected at least 1 transaction")
	}
}

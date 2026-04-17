package publisher_billing_test

import (
	"context"
	"testing"

	"github.com/adortb/adortb-billing/internal/metrics"
	"github.com/adortb/adortb-billing/internal/publisher_billing"
	"github.com/adortb/adortb-billing/internal/repo"
	"github.com/prometheus/client_golang/prometheus"
)

func newPubService() *publisher_billing.Service {
	m := metrics.New(prometheus.NewRegistry())
	return publisher_billing.NewService(repo.NewMockPublisherRepo(), m)
}

func TestGetAccount_Create(t *testing.T) {
	svc := newPubService()
	acc, err := svc.GetAccount(context.Background(), 1001)
	if err != nil {
		t.Fatal(err)
	}
	if acc.PublisherID != 1001 {
		t.Errorf("expected publisher_id 1001")
	}
}

func TestAddRevenue(t *testing.T) {
	svc := newPubService()
	ctx := context.Background()

	tx, err := svc.AddRevenue(ctx, 2001, 9.0, "event", "evt-001", "test")
	if err != nil {
		t.Fatalf("add revenue error: %v", err)
	}
	if tx.Amount != 9.0 {
		t.Errorf("expected 9.0")
	}

	acc, _ := svc.GetAccount(ctx, 2001)
	if acc.RevenuePending != 9.0 {
		t.Errorf("expected pending 9.0, got %v", acc.RevenuePending)
	}
}

func TestCreateWithdraw_InsufficientBalance(t *testing.T) {
	ctx := context.Background()

	// 没有 settled 余额
	_, err := newPubService().CreateWithdraw(ctx, 3001, 100.0, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateWithdraw_Success(t *testing.T) {
	ctx := context.Background()

	mockRepo := repo.NewMockPublisherRepo()
	m := metrics.New(prometheus.NewRegistry())
	svcWithRepo := publisher_billing.NewService(mockRepo, m)

	// 注入已有 settled 余额
	mockRepo.InjectSettled(4001, 200.0)

	wr, err := svcWithRepo.CreateWithdraw(ctx, 4001, 150.0, []byte(`{"bank":"ABC"}`))
	if err != nil {
		t.Fatalf("withdraw error: %v", err)
	}
	if wr.Amount != 150.0 {
		t.Errorf("expected amount 150.0")
	}
}

func TestApproveWithdraw(t *testing.T) {
	mockRepo := repo.NewMockPublisherRepo()
	m := metrics.New(prometheus.NewRegistry())
	svc := publisher_billing.NewService(mockRepo, m)
	ctx := context.Background()

	mockRepo.InjectSettled(5001, 100.0)
	wr, err := svc.CreateWithdraw(ctx, 5001, 50.0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApproveWithdraw(ctx, wr.ID, 9999); err != nil {
		t.Fatalf("approve error: %v", err)
	}

	updated, _ := svc.GetWithdrawRequest(ctx, wr.ID)
	if updated.Status != "approved" {
		t.Errorf("expected status approved, got %v", updated.Status)
	}
}

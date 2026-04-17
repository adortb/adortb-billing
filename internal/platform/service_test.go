package platform_test

import (
	"context"
	"testing"

	"github.com/adortb/adortb-billing/internal/platform"
	"github.com/adortb/adortb-billing/internal/repo"
)

func TestCalcFees_DefaultRate(t *testing.T) {
	svc := platform.NewService(repo.NewMockPlatformRepo(0.10))
	ctx := context.Background()

	fee, net, err := svc.CalcFees(ctx, 10.0)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 1.0 {
		t.Errorf("expected fee 1.0, got %v", fee)
	}
	if net != 9.0 {
		t.Errorf("expected net 9.0, got %v", net)
	}
}

func TestCalcFees_ZeroRevenue(t *testing.T) {
	svc := platform.NewService(repo.NewMockPlatformRepo(0.10))
	ctx := context.Background()

	fee, net, err := svc.CalcFees(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 0 || net != 0 {
		t.Errorf("expected 0,0, got %v,%v", fee, net)
	}
}

func TestCalcFees_Rounding(t *testing.T) {
	svc := platform.NewService(repo.NewMockPlatformRepo(0.10))
	ctx := context.Background()

	fee, net, err := svc.CalcFees(ctx, 1.0/3.0) // 0.3333...
	if err != nil {
		t.Fatal(err)
	}
	if fee+net < 0.3332 || fee+net > 0.3335 {
		t.Errorf("rounding mismatch fee=%v net=%v sum=%v", fee, net, fee+net)
	}
}

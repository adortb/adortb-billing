package pacing

import (
	"testing"
	"time"
)

// ─── TrafficCurve ──────────────────────────────────────────────────────────

func TestDefaultCurve_SumsToOne(t *testing.T) {
	c := DefaultCurve()
	for day := 0; day < 7; day++ {
		total := c.CumulativeFraction(day, 23)
		// 允许浮点误差
		if total < 0.99 || total > 1.01 {
			t.Errorf("weekday %d 全天累计 = %.4f，期望 ~1.0", day, total)
		}
	}
}

func TestCurve_CumulativeFraction_Monotonic(t *testing.T) {
	c := DefaultCurve()
	prev := 0.0
	for h := 0; h < 24; h++ {
		cur := c.CumulativeFraction(1, h) // Monday
		if cur < prev {
			t.Errorf("累计比例在 hour=%d 时减小：%.4f < %.4f", h, cur, prev)
		}
		prev = cur
	}
}

func TestCurve_OutOfRange(t *testing.T) {
	c := DefaultCurve()
	if got := c.CumulativeFraction(-1, 0); got != 0 {
		t.Errorf("weekday=-1 应返回 0，实际 %v", got)
	}
	if got := c.CumulativeFraction(7, 0); got != 0 {
		t.Errorf("weekday=7 应返回 0，实际 %v", got)
	}
	if got := c.CumulativeFraction(0, 24); got != 0 {
		t.Errorf("hour=24 应返回 0，实际 %v", got)
	}
}

func TestCurve_Learn(t *testing.T) {
	c := DefaultCurve()
	before := c.HourFraction(1, 12)
	c.Learn(1, 12, 0.2) // 推高 hour=12 权重
	after := c.HourFraction(1, 12)
	if after <= before {
		t.Errorf("Learn 后 hour=12 权重应增加：before=%.4f after=%.4f", before, after)
	}
	// 学习后曲线仍归一化
	total := c.CumulativeFraction(1, 23)
	if total < 0.99 || total > 1.01 {
		t.Errorf("Learn 后全天累计 = %.4f，期望 ~1.0", total)
	}
}

// ─── Factor ───────────────────────────────────────────────────────────────

func TestFactor_OnTrack(t *testing.T) {
	c := DefaultCurve()
	// 构造一个固定时刻：Monday 08:00
	now := time.Date(2026, 4, 20, 8, 0, 0, 0, time.UTC)
	expectedFrac := c.CumulativeFraction(int(now.Weekday()), now.Hour())
	spent := 100.0 * expectedFrac
	f := Factor(100.0, spent, c, now)
	// 消耗 = 预期 → factor ≈ 1.0
	if f < 0.95 || f > 1.05 {
		t.Errorf("按计划消耗时 Factor = %.3f，期望 ~1.0", f)
	}
}

func TestFactor_Underspending(t *testing.T) {
	c := DefaultCurve()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	// 实际消耗仅为预期的 50%
	expectedFrac := c.CumulativeFraction(int(now.Weekday()), now.Hour())
	spent := 100.0 * expectedFrac * 0.5
	f := Factor(100.0, spent, c, now)
	if f <= 1.0 {
		t.Errorf("消耗不足时 Factor = %.3f，期望 > 1.0（加速）", f)
	}
}

func TestFactor_Overspending(t *testing.T) {
	c := DefaultCurve()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	// 实际消耗是预期的 2 倍
	expectedFrac := c.CumulativeFraction(int(now.Weekday()), now.Hour())
	spent := 100.0 * expectedFrac * 2.0
	f := Factor(100.0, spent, c, now)
	if f >= 1.0 {
		t.Errorf("超量消耗时 Factor = %.3f，期望 < 1.0（减速）", f)
	}
}

func TestFactor_ZeroBudget(t *testing.T) {
	c := DefaultCurve()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	f := Factor(0, 10.0, c, now)
	if f != 1.0 {
		t.Errorf("日预算=0 时 Factor = %.3f，期望 1.0", f)
	}
}

func TestFactor_Clamped(t *testing.T) {
	c := DefaultCurve()
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	// 超量消耗 → factor 不低于 minFactor
	f := Factor(100.0, 10000.0, c, now)
	if f < minFactor {
		t.Errorf("Factor = %.3f 低于 minFactor=%v", f, minFactor)
	}

	// 零消耗 → factor 不超过 maxFactor
	f = Factor(100.0, 0, c, now)
	if f > maxFactor {
		t.Errorf("Factor = %.3f 超过 maxFactor=%v", f, maxFactor)
	}
}

// ─── Tracker ─────────────────────────────────────────────────────────────

func TestTracker_UnknownCampaign(t *testing.T) {
	tr := NewTracker(nil)
	if f := tr.Factor(9999); f != 1.0 {
		t.Errorf("未知 campaign Factor = %.3f，期望 1.0", f)
	}
}

func TestTracker_SetAndFactor(t *testing.T) {
	c := DefaultCurve()
	tr := NewTracker(c)
	tr.SetBudget(1, 100.0)

	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	expFrac := c.CumulativeFraction(int(now.Weekday()), now.Hour())
	tr.SetSpent(1, 100.0*expFrac)

	f := tr.FactorAt(1, now)
	if f < 0.95 || f > 1.05 {
		t.Errorf("按计划消耗 FactorAt = %.3f，期望 ~1.0", f)
	}
}

func TestTracker_AddSpend(t *testing.T) {
	tr := NewTracker(nil)
	tr.SetBudget(2, 100.0)
	tr.SetSpent(2, 10.0)
	tr.AddSpend(2, 5.0)

	st, ok := tr.State(2)
	if !ok {
		t.Fatal("State 应返回存在的 campaign")
	}
	if st.Spent != 15.0 {
		t.Errorf("AddSpend 后 Spent = %.2f，期望 15.0", st.Spent)
	}
}

func TestTracker_AddSpendUnknownCampaign(t *testing.T) {
	tr := NewTracker(nil)
	// 对未知 campaign AddSpend 不应 panic
	tr.AddSpend(9999, 5.0)
	_, ok := tr.State(9999)
	if ok {
		t.Error("未知 campaign AddSpend 后不应创建 state")
	}
}

func TestTracker_StateSnapshot(t *testing.T) {
	tr := NewTracker(nil)
	tr.SetBudget(3, 200.0)
	tr.SetSpent(3, 50.0)

	st, ok := tr.State(3)
	if !ok {
		t.Fatal("State 应返回数据")
	}
	if st.DailyBudget != 200.0 || st.Spent != 50.0 {
		t.Errorf("State = %+v，期望 DailyBudget=200 Spent=50", st)
	}
}

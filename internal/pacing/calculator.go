package pacing

import "time"

const (
	// minFactor 最低 pacing factor（极度超速时）
	minFactor = 0.0
	// maxFactor 最高 pacing factor（极度滞后时）
	maxFactor = 2.0
)

// Factor 计算给定时刻的 pacing factor。
//
// 返回值含义：
//   - < 1.0：实际消耗快于计划，DSP 应降低出价或减少 QPS（减速）
//   - = 1.0：消耗与计划一致（正常）
//   - > 1.0：实际消耗慢于计划，DSP 应提高出价或增加 QPS（加速）
//
// 边界处理：dailyBudget <= 0 或曲线预期为 0 时返回 1.0（不干预）。
func Factor(dailyBudget, currentSpent float64, curve *TrafficCurve, now time.Time) float64 {
	if dailyBudget <= 0 {
		return 1.0
	}
	weekday := int(now.Weekday())
	hour := now.Hour()

	expectedFrac := curve.CumulativeFraction(weekday, hour)
	if expectedFrac <= 0 {
		if currentSpent <= 0 {
			return 1.0
		}
		// 早期（expectedFrac=0）就有消耗，需要减速
		return minFactor
	}

	expectedSpent := dailyBudget * expectedFrac
	ratio := currentSpent / expectedSpent
	if ratio <= 0 {
		return maxFactor
	}
	// factor = 1 / ratio：ratio>1 说明超速，factor<1；ratio<1 说明滞后，factor>1
	factor := 1.0 / ratio
	return clamp(factor)
}

func clamp(v float64) float64 {
	if v < minFactor {
		return minFactor
	}
	if v > maxFactor {
		return maxFactor
	}
	return v
}

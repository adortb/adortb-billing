// Package pacing 实现 Smart Pacing（智能预算节奏），
// 确保日预算在 24 小时内匀速消耗，避免早期花光或末期剩余。
package pacing

import (
	"sync"
	"time"
)

// TrafficCurve 存储 7 × 24 小时流量分布矩阵。
// matrix[weekday][hour] = 该时段占全天流量的比例，每行之和为 1.0。
// weekday 遵循 Go time.Weekday 定义：0=Sunday, 1=Monday, ..., 6=Saturday。
type TrafficCurve struct {
	mu     sync.RWMutex
	matrix [7][24]float64
}

// DefaultCurve 返回基于典型互联网广告流量模式的默认曲线：
// 凌晨低谷，午间（12点）和晚间（20-21点）双峰。
func DefaultCurve() *TrafficCurve {
	c := &TrafficCurve{}
	// 通用工作日流量分布（7 天使用相同基准，可通过 Learn 个性化）
	base := [24]float64{
		0.010, 0.008, 0.007, 0.006, 0.007, 0.010, // 00-05
		0.020, 0.035, 0.050, 0.055, 0.060, 0.055, // 06-11
		0.060, 0.055, 0.050, 0.048, 0.045, 0.048, // 12-17
		0.055, 0.062, 0.065, 0.060, 0.048, 0.026, // 18-23
	}
	for day := range c.matrix {
		c.matrix[day] = base
	}
	// 周末（0=Sunday, 6=Saturday）流量高峰稍晚
	weekend := [24]float64{
		0.012, 0.010, 0.008, 0.007, 0.007, 0.009, // 00-05
		0.015, 0.025, 0.040, 0.050, 0.055, 0.058, // 06-11
		0.062, 0.060, 0.055, 0.052, 0.052, 0.055, // 12-17
		0.060, 0.065, 0.068, 0.062, 0.050, 0.028, // 18-23
	}
	c.matrix[time.Sunday] = weekend
	c.matrix[time.Saturday] = weekend
	// 确保每行归一化为 1.0
	for day := range c.matrix {
		c.normalize(day)
	}
	return c
}

// CumulativeFraction 返回 weekday 当天截止 untilHour（含）时刻，
// 预期已消耗的流量比例（∑ matrix[weekday][0..untilHour]）。
// weekday ∈ [0,6], untilHour ∈ [0,23]。
func (c *TrafficCurve) CumulativeFraction(weekday, untilHour int) float64 {
	if weekday < 0 || weekday > 6 || untilHour < 0 || untilHour > 23 {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	sum := 0.0
	for h := 0; h <= untilHour; h++ {
		sum += c.matrix[weekday][h]
	}
	return sum
}

// HourFraction 返回指定时段的流量占比。
func (c *TrafficCurve) HourFraction(weekday, hour int) float64 {
	if weekday < 0 || weekday > 6 || hour < 0 || hour > 23 {
		return 0
	}
	c.mu.RLock()
	v := c.matrix[weekday][hour]
	c.mu.RUnlock()
	return v
}

// Learn 更新指定时段的流量比例（指数移动平均，α=0.1 缓慢适应）。
// 每次调用后内部自动归一化保证行和为 1.0。
func (c *TrafficCurve) Learn(weekday, hour int, observedFrac float64) {
	if weekday < 0 || weekday > 6 || hour < 0 || hour > 23 || observedFrac < 0 {
		return
	}
	const alpha = 0.1
	c.mu.Lock()
	old := c.matrix[weekday][hour]
	c.matrix[weekday][hour] = old*(1-alpha) + observedFrac*alpha
	c.normalize(weekday)
	c.mu.Unlock()
}

// normalize 确保 matrix[weekday] 之和为 1.0（防止浮点累计漂移）。
// 必须在持有写锁时调用。
func (c *TrafficCurve) normalize(weekday int) {
	sum := 0.0
	for h := 0; h < 24; h++ {
		sum += c.matrix[weekday][h]
	}
	if sum <= 0 {
		return
	}
	for h := 0; h < 24; h++ {
		c.matrix[weekday][h] /= sum
	}
}

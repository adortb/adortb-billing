package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	"github.com/adortb/adortb-billing/internal/advertiser_billing"
	"github.com/adortb/adortb-billing/internal/metrics"
	"github.com/adortb/adortb-billing/internal/platform"
	"github.com/adortb/adortb-billing/internal/publisher_billing"
)

const (
	topicEvents = "adortb.events"
	groupID     = "adortb-billing"
)

// AdEvent 从 Kafka 消费的广告事件结构
type AdEvent struct {
	Type        string  `json:"type"`
	SlotID      string  `json:"slot_id"`
	CampaignID  string  `json:"campaign_id"`
	AdvertiserID int64  `json:"advertiser_id"`
	PublisherID  int64  `json:"publisher_id"`
	BidPrice    float64 `json:"bid_price"`
	EventID     string  `json:"event_id"`
	Timestamp   int64   `json:"timestamp"`
}

// Consumer Kafka 消费者：消费事件 → 触发计费逻辑
type Consumer struct {
	advSvc  *advertiser_billing.Service
	pubSvc  *publisher_billing.Service
	platSvc *platform.Service
	metrics *metrics.Metrics
	brokers []string
}

func NewConsumer(
	brokers []string,
	advSvc *advertiser_billing.Service,
	pubSvc *publisher_billing.Service,
	platSvc *platform.Service,
	m *metrics.Metrics,
) *Consumer {
	return &Consumer{
		advSvc:  advSvc,
		pubSvc:  pubSvc,
		platSvc: platSvc,
		metrics: m,
		brokers: brokers,
	}
}

// Start 启动 Kafka consumer group，ctx 取消时优雅退出
func (c *Consumer) Start(ctx context.Context) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_8_0_0
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	cfg.Consumer.Return.Errors = true

	client, err := sarama.NewConsumerGroup(c.brokers, groupID, cfg)
	if err != nil {
		return fmt.Errorf("new consumer group: %w", err)
	}
	defer client.Close()

	handler := &consumerGroupHandler{consumer: c}

	for {
		if err := client.Consume(ctx, []string{topicEvents}, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("consumer group error", "err", err)
			c.metrics.RecordKafkaError()
			time.Sleep(2 * time.Second)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type consumerGroupHandler struct {
	consumer *Consumer
}

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.consumer.processMessage(sess.Context(), msg); err != nil {
			slog.Error("process billing event", "err", err, "offset", msg.Offset)
			h.consumer.metrics.RecordKafkaError()
		}
		sess.MarkMessage(msg, "")
	}
	return nil
}

func (c *Consumer) processMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var evt AdEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		return fmt.Errorf("unmarshal event: %w", err)
	}

	c.metrics.RecordKafkaEvent(evt.Type)

	switch evt.Type {
	case "impression", "viewable":
		return c.handleImpressionEvent(ctx, &evt)
	case "click":
		return c.handleClickEvent(ctx, &evt)
	default:
		return nil
	}
}

func (c *Consumer) handleImpressionEvent(ctx context.Context, evt *AdEvent) error {
	if evt.BidPrice <= 0 || evt.AdvertiserID == 0 {
		return nil
	}

	// 1. 广告主扣费
	if err := c.advSvc.SpendForEvent(ctx, evt.AdvertiserID, evt.BidPrice, evt.EventID); err != nil {
		return fmt.Errorf("advertiser spend: %w", err)
	}

	// 2. 计算平台分成，更新媒体方收入
	if evt.PublisherID != 0 {
		platformFee, netRevenue, err := c.platSvc.CalcFees(ctx, evt.BidPrice)
		if err != nil {
			return fmt.Errorf("calc fees: %w", err)
		}
		_ = platformFee // 平台收入通过 daily_settlements 汇总，不单独入账
		desc := fmt.Sprintf("impression 收入 slot=%s campaign=%s", evt.SlotID, evt.CampaignID)
		if _, err := c.pubSvc.AddRevenue(ctx, evt.PublisherID, netRevenue,
			string("event"), evt.EventID, desc); err != nil {
			return fmt.Errorf("publisher revenue: %w", err)
		}
	}
	return nil
}

func (c *Consumer) handleClickEvent(ctx context.Context, evt *AdEvent) error {
	// CPC 场景：与 impression 逻辑相同，按点击扣费
	return c.handleImpressionEvent(ctx, evt)
}

package account

import "time"

type AccountType string

const (
	AccountTypeAdvertiser AccountType = "advertiser"
	AccountTypePublisher  AccountType = "publisher"
	AccountTypePlatform   AccountType = "platform"
)

type TxType string

const (
	TxTypeRecharge    TxType = "recharge"
	TxTypeSpend       TxType = "spend"
	TxTypeRevenue     TxType = "revenue"
	TxTypeSettlement  TxType = "settlement"
	TxTypeWithdraw    TxType = "withdraw"
	TxTypeRefund      TxType = "refund"
	TxTypePlatformFee TxType = "platform_fee"
)

type RefType string

const (
	RefTypeEvent          RefType = "event"
	RefTypeCampaign       RefType = "campaign"
	RefTypeWithdrawRequest RefType = "withdraw_request"
)

type BalanceTransaction struct {
	ID           int64       `json:"id"`
	AccountType  AccountType `json:"account_type"`
	AccountID    int64       `json:"account_id"`
	TxType       TxType      `json:"tx_type"`
	Amount       float64     `json:"amount"`
	BalanceAfter float64     `json:"balance_after"`
	RefType      string      `json:"ref_type,omitempty"`
	RefID        string      `json:"ref_id,omitempty"`
	Description  string      `json:"description,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

type AdvertiserAccount struct {
	AdvertiserID   int64     `json:"advertiser_id"`
	Balance        float64   `json:"balance"`
	Frozen         float64   `json:"frozen"`
	TotalRecharge  float64   `json:"total_recharge"`
	TotalSpent     float64   `json:"total_spent"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PublisherAccount struct {
	PublisherID       int64     `json:"publisher_id"`
	RevenuePending    float64   `json:"revenue_pending"`
	RevenueSettled    float64   `json:"revenue_settled"`
	RevenueWithdrawn  float64   `json:"revenue_withdrawn"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type WithdrawStatus string

const (
	WithdrawStatusPending  WithdrawStatus = "pending"
	WithdrawStatusApproved WithdrawStatus = "approved"
	WithdrawStatusRejected WithdrawStatus = "rejected"
	WithdrawStatusPaid     WithdrawStatus = "paid"
)

type WithdrawRequest struct {
	ID          int64          `json:"id"`
	PublisherID int64          `json:"publisher_id"`
	Amount      float64        `json:"amount"`
	Status      WithdrawStatus `json:"status"`
	BankInfo    []byte         `json:"bank_info,omitempty"`
	ReviewedBy  *int64         `json:"reviewed_by,omitempty"`
	ReviewedAt  *time.Time     `json:"reviewed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type DailySettlement struct {
	ID           int64   `json:"id"`
	Date         string  `json:"date"`
	AdvertiserID *int64  `json:"advertiser_id,omitempty"`
	PublisherID  *int64  `json:"publisher_id,omitempty"`
	CampaignID   *int64  `json:"campaign_id,omitempty"`
	Impressions  int64   `json:"impressions"`
	Clicks       int64   `json:"clicks"`
	GrossRevenue float64 `json:"gross_revenue"`
	PlatformFee  float64 `json:"platform_fee"`
	NetRevenue   float64 `json:"net_revenue"`
}

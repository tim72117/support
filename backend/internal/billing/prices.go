package billing

import "github.com/tim72117/ai-support/internal/quota"

// Price is what one billing period of a tier costs. This table — not
// anything the browser sends — decides every charged amount.
type Price struct {
	Tier   quota.Tier
	Name   string // shown to the cardholder as the charge's details
	Amount int    // whole TWD, tax-inclusive
}

// PLACEHOLDER PRICES, matching the demo landing page. Replace with the real
// pricing before going live (and decide tax-inclusive vs not).
var prices = map[quota.Tier]Price{
	quota.TierStarter:  {Tier: quota.TierStarter, Name: "參選起步（月繳）", Amount: 990},
	quota.TierCampaign: {Tier: quota.TierCampaign, Name: "競選衝刺（月繳）", Amount: 2990},
}

func PriceFor(t quota.Tier) (Price, bool) {
	p, ok := prices[t]
	return p, ok
}

// Prices returns the sellable plans in a stable order (cheapest first).
func Prices() []Price {
	return []Price{prices[quota.TierStarter], prices[quota.TierCampaign]}
}

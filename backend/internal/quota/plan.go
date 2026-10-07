package quota

// This file is the single place plans are defined. A plan maps a
// subscription tier to the concrete limits that tier grants. Quota
// enforcement (quota.go) resolves a user's tier to a Plan here at check
// time and never reads a per-row copy of the number — so changing a plan's
// MonthlyTokens below immediately applies to every user on that tier, with
// no migration and no backfill. Add a new paid tier by adding one entry to
// plans and (optionally) a matching Tier constant.

// Tier is a subscription tier identifier, stored as subscriptions.tier.
// Kept as a plain string (not a DB enum) so a new tier is a code change
// here, never a schema migration.
type Tier string

const (
	// TierFree is the tier every account starts on at signup, and the tier
	// any user with no subscriptions row is treated as.
	TierFree Tier = "free"

	// TierStarter / TierCampaign are the paid tiers sold on the candidate
	// landing page (參選起步 / 競選衝刺). Assigned when a subscription is
	// paid for (or by an admin via SetTier).
	TierStarter  Tier = "starter"
	TierCampaign Tier = "campaign"

	// TierBizStarter / TierBizGrowth are the paid tiers sold on the
	// business landing page (輕量起步 / 成長方案). Separate identifiers from
	// TierStarter/TierCampaign because the two product lines have different
	// pricing and limits even where a name is similar.
	TierBizStarter Tier = "biz_starter"
	TierBizGrowth  Tier = "biz_growth"

	// TierCandidateTrial is the 7-day free trial offered on the candidate
	// landing page. No card is collected and nothing is ever charged for
	// it: billing.Service.StartTrial assigns this tier directly, with
	// billing_profiles.current_period_end set to now+7 days. When that
	// date passes, billing.Service.Get/the subscription status reports the
	// trial as expired; the owner must then subscribe normally (through
	// the existing card-collecting Subscribe flow, to TierStarter or
	// TierCampaign) to keep paid access — there is no automatic conversion
	// or charge. Kept as its own tier (rather than aliasing TierCampaign)
	// so a trial user is distinguishable in subscriptions.tier /
	// billing_profiles.tier for support and reporting, and so quota can
	// grant it its own allowance.
	TierCandidateTrial Tier = "candidate_trial"
)

// DefaultTier is assigned to new accounts and assumed for any user whose
// tier can't be resolved (missing row, or a stored tier no longer defined
// in plans — see PlanFor).
const DefaultTier = TierFree

// Plan is the set of limits a tier grants. Today that's just a monthly
// token allowance; new limit dimensions (concurrent sessions, ...) are added
// as fields here and read wherever they apply.
type Plan struct {
	// Tier is the identifier this plan is keyed by; stored on the user's
	// subscriptions row.
	Tier Tier
	// Name is a human-readable label for UIs and CLI output (e.g. "Free").
	Name string
	// MonthlyTokens is how many LLM tokens (prompt + completion, summed from
	// usage_events.total_tokens) this tier includes per billing period. This
	// is THE quota number — editing it here changes the allowance for every
	// user on this tier at once. Replaces the earlier per-prompt-count model
	// (MonthlyPrompts): a plan of N prompts was a poor proxy for actual LLM
	// cost once a single prompt could trigger a variable number of internal
	// provider round-trips (tool-calling loops) each with very different
	// token weight — see quota.usageSince's doc comment.
	MonthlyTokens int
}

// plans is the authoritative table of every defined plan, keyed by tier.
var plans = map[Tier]Plan{
	TierFree: {
		Tier:          TierFree,
		Name:          "Free",
		MonthlyTokens: 100_000,
	},
	// The allowance numbers below are PLACEHOLDERS until the real pricing
	// and usage unit are decided.
	TierStarter: {
		Tier:          TierStarter,
		Name:          "參選起步",
		MonthlyTokens: 1_000_000,
	},
	TierCampaign: {
		Tier:          TierCampaign,
		Name:          "競選衝刺",
		MonthlyTokens: 10_000_000,
	},
	TierBizStarter: {
		Tier:          TierBizStarter,
		Name:          "輕量起步",
		MonthlyTokens: 1_000_000,
	},
	TierBizGrowth: {
		Tier:          TierBizGrowth,
		Name:          "成長方案",
		MonthlyTokens: 10_000_000,
	},
	TierCandidateTrial: {
		Tier: TierCandidateTrial,
		Name: "7 天免費試用",
		// Same allowance as TierStarter (500 conversations' worth, per the
		// landing page copy) — the trial is meant to let a candidate try the
		// product, not hand out the flagship plan's full quota for free.
		MonthlyTokens: 1_000_000,
	},
}

// FreePlan is the free tier's plan, the fallback used whenever a specific
// tier can't be resolved. Guaranteed to exist (TierFree is always in plans).
var FreePlan = plans[TierFree]

// PlanFor returns the plan for tier, falling back to the free plan for any
// tier not present in plans. The fallback matters for forward/backward
// safety: a subscriptions row could hold a tier written by a newer build
// (a paid tier since removed, or a typo from a manual UPDATE); resolving it
// to Free fails safe (least privilege) rather than erroring or granting
// unlimited access.
func PlanFor(tier Tier) Plan {
	if p, ok := plans[tier]; ok {
		return p
	}
	return FreePlan
}

// AllPlans returns every defined plan. Order is not guaranteed (map
// iteration); callers that need a stable order should sort. Intended for
// surfacing the plan catalog to a console/CLI.
func AllPlans() []Plan {
	out := make([]Plan, 0, len(plans))
	for _, p := range plans {
		out = append(out, p)
	}
	return out
}

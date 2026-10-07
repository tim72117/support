// Shapes returned by backend/internal/admin — see admin.go's ownerSummary
// and business.Business (serialised as-is, hence the capitalised field
// names on OwnerBusiness, matching apps/console's own ApiBusiness).

export interface OwnerSummary {
  id: number
  email: string
  businessCount: number
  tier?: string
  planName?: string
  limit: number
  used: number
  billingStatus?: string
  cardLastFour?: string
  currentPeriodEnd?: string
}

export interface OwnerBusiness {
  ID: number
  OwnerID: number
  Slug: string
  Name: string
  Tagline: string
  Mascot: string
  ThemeColor: string
  Layout: string
  Connected: boolean
}

package account

// Unlimited marks a resource as uncapped. It is the zero value, so a Limits
// field left unset means "no cap" — which is exactly the Pro tier.
const Unlimited = 0

// Limits is the per-tier cap on the countable resources an account owns. A
// field set to Unlimited is not enforced.
type Limits struct {
	Databases  int
	Seats      int // members + pending invitations
	APIKeys    int // active (non-revoked) keys
	Webhooks   int
	Heartbeats int // backup check-in monitors
}

// LimitsFor returns the resource caps for a plan tier. Scale is the
// uncapped self-serve top tier; Growth (PlanPro) and Starter cap by tier.
// Trial mirrors Growth so prospects experience the daily-cadence tier
// during their first month. Unknown plans fall to the most restrictive
// caps so a bad value can never widen access.
func LimitsFor(p Plan) Limits {
	switch p {
	case PlanScale:
		return Limits{} // all Unlimited — self-serve top tier
	case PlanPro, PlanTrial:
		return Limits{Databases: 25, Seats: 10, APIKeys: 10, Webhooks: 10, Heartbeats: 25}
	case PlanStarter:
		return Limits{Databases: 5, Seats: 3, APIKeys: 3, Webhooks: 3, Heartbeats: 10}
	default:
		return Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1, Heartbeats: 1}
	}
}

// AllowedCadences returns the drill cadences a plan may select, from least
// to most frequent. Starter tops out at weekly; Growth (PlanPro) adds
// daily; Scale unlocks hourly. Trial mirrors Growth so prospects
// experience the daily cadence during their first month.
func AllowedCadences(p Plan) []string {
	switch p {
	case PlanScale:
		return []string{"off", "monthly", "weekly", "daily", "hourly"}
	case PlanPro, PlanTrial:
		return []string{"off", "monthly", "weekly", "daily"}
	case PlanStarter:
		return []string{"off", "monthly", "weekly"}
	default:
		return []string{"off", "monthly", "weekly"}
	}
}

// CadenceAllowed reports whether a plan may schedule drills at a cadence.
func CadenceAllowed(p Plan, cadence string) bool {
	for _, c := range AllowedCadences(p) {
		if c == cadence {
			return true
		}
	}
	return false
}

// TopCadence is the most frequent cadence a plan allows — the headline the
// pricing page leads with.
func TopCadence(p Plan) string {
	allowed := AllowedCadences(p)
	return allowed[len(allowed)-1]
}

// AtLimit reports whether an account already holding `count` of a resource
// has reached a cap of `limit` — i.e. creating one more is not allowed. An
// Unlimited cap never blocks.
func AtLimit(count, limit int) bool {
	return limit != Unlimited && count >= limit
}

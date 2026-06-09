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

// LimitsFor returns the resource caps for a plan tier. The VIP tier
// (PlanPro) is uncapped; the trial gets the same generous caps as
// Standard (PlanStarter) for its window; an unknown plan falls to the
// most restrictive caps so a bad value can never widen access.
func LimitsFor(p Plan) Limits {
	switch p {
	case PlanPro:
		return Limits{} // all Unlimited
	case PlanStarter, PlanTrial:
		return Limits{Databases: 10, Seats: 10, APIKeys: 5, Webhooks: 5, Heartbeats: 20}
	default:
		return Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1, Heartbeats: 1}
	}
}

// AllowedCadences returns the drill cadences a plan may select, from least to
// most frequent. Standard (PlanStarter) tops out at weekly; VIP (PlanPro)
// adds daily. Trial mirrors VIP so prospects can experience the top cadence
// during the trial window. Hourly is reserved for enterprise / custom and
// is not exposed by any standard tier.
func AllowedCadences(p Plan) []string {
	switch p {
	case PlanPro, PlanTrial:
		return []string{"off", "weekly", "daily"}
	case PlanStarter:
		return []string{"off", "weekly"}
	default:
		return []string{"off", "weekly"}
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

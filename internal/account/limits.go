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
	// DrillsPerDay caps drills created (any origin: sample, web, API, schedule)
	// per account per rolling 24h. Prevents a single tenant from monopolising
	// the shared River queue and racking up the marginal drill cost.
	DrillsPerDay int
	// MaxDumpBytes caps the on-disk size of a dump accepted for restore. We
	// stat the source at create time and reject before a large drill burns
	// the 30-minute restore timeout and River retries the whole fetch+restore.
	// int64 so tables can express TB without overflow.
	MaxDumpBytes int64
}

// LimitsFor returns the resource caps for a plan tier. Scale is the
// uncapped self-serve top tier; Growth (PlanPro) and Starter cap by tier.
// Trial mirrors Growth so prospects experience the daily-cadence tier
// during their first month. Unknown plans fall to the most restrictive
// caps so a bad value can never widen access.
func LimitsFor(p Plan) Limits {
	const gb = int64(1) << 30
	switch p {
	case PlanScale:
		// Scale is unlimited on Databases/Seats/etc, but drills and dumps
		// still carry hard ceilings — protects the shared queue and prevents
		// unbounded-cost restore runs from a single tenant.
		return Limits{DrillsPerDay: 500, MaxDumpBytes: 1024 * gb}
	case PlanPro:
		return Limits{Databases: 25, Seats: 10, APIKeys: 10, Webhooks: 10, Heartbeats: 25,
			DrillsPerDay: 100, MaxDumpBytes: 200 * gb}
	case PlanStarter:
		return Limits{Databases: 5, Seats: 3, APIKeys: 3, Webhooks: 3, Heartbeats: 10,
			DrillsPerDay: 20, MaxDumpBytes: 20 * gb}
	case PlanTrial:
		// Active trials get ONE real database at weekly cadence — the
		// product's whole thesis is "prove it, don't promise it", so a card
		// wall before someone can drill their own dump is self-refuting. Two
		// seats let a small evaluating team walk the flow together; the
		// paywall re-arms once the trial lapses. Drills/day is tight: enough
		// to iterate on assertions for one DB, not enough to hammer the queue.
		return Limits{Databases: 1, Seats: 2, APIKeys: 2, Webhooks: 2, Heartbeats: 3,
			DrillsPerDay: 5, MaxDumpBytes: 5 * gb}
	default:
		return Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1, Heartbeats: 1,
			DrillsPerDay: 2, MaxDumpBytes: 1 * gb}
	}
}

// AllowedCadences returns the drill cadences a plan may select, from least
// to most frequent. Starter and Growth both top out at weekly — they
// differ on database/drill volume, not frequency. Daily is the headline
// reason to move up to Scale. Trial mirrors Growth (weekly). Hourly /
// sub-daily is an Enterprise (custom) arrangement, not a self-serve option.
func AllowedCadences(p Plan) []string {
	switch p {
	case PlanScale:
		return []string{"off", "monthly", "weekly", "daily"}
	case PlanStarter, PlanPro, PlanTrial:
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

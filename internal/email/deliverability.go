package email

import (
	"context"
	"time"
)

// Deliverability is the aggregate email-health snapshot for the staff
// deliverability report.
type Deliverability struct {
	Sends7d       int
	Sends30d      int
	Suppressed30d int
	SuppressedAll int
	// BounceRate30d is suppressions ÷ sends over the last 30 days, as a
	// percentage. Zero when nothing was sent in the window.
	BounceRate30d float64
	ByReason      []ReasonCount
	Recent        []Suppression
}

// ReasonCount is a suppression reason and how many addresses carry it over
// the last 30 days.
type ReasonCount struct {
	Reason string
	Count  int
}

// Suppression is one suppressed address, for the recent-suppressions list.
type Suppression struct {
	Email        string
	Reason       string
	SuppressedAt time.Time
}

// Deliverability gathers the email-health report: how much was sent, how
// much bounced or drew a complaint, and the most recent suppressions.
func (m *Mailer) Deliverability(ctx context.Context) (Deliverability, error) {
	var d Deliverability
	if err := m.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE sent_at > now() - interval '7 days'),
		  count(*) FILTER (WHERE sent_at > now() - interval '30 days')
		FROM email_sends
	`).Scan(&d.Sends7d, &d.Sends30d); err != nil {
		return Deliverability{}, err
	}
	if err := m.pool.QueryRow(ctx, `
		SELECT
		  count(*),
		  count(*) FILTER (WHERE suppressed_at > now() - interval '30 days')
		FROM email_suppressions
	`).Scan(&d.SuppressedAll, &d.Suppressed30d); err != nil {
		return Deliverability{}, err
	}
	if d.Sends30d > 0 {
		d.BounceRate30d = float64(d.Suppressed30d) / float64(d.Sends30d) * 100
	}

	reasons, err := m.pool.Query(ctx, `
		SELECT reason, count(*)
		  FROM email_suppressions
		 WHERE suppressed_at > now() - interval '30 days'
		 GROUP BY reason
		 ORDER BY count(*) DESC
	`)
	if err != nil {
		return Deliverability{}, err
	}
	for reasons.Next() {
		var rc ReasonCount
		if err := reasons.Scan(&rc.Reason, &rc.Count); err != nil {
			reasons.Close()
			return Deliverability{}, err
		}
		d.ByReason = append(d.ByReason, rc)
	}
	reasons.Close()
	if err := reasons.Err(); err != nil {
		return Deliverability{}, err
	}

	recent, err := m.pool.Query(ctx, `
		SELECT email::text, reason, suppressed_at
		  FROM email_suppressions
		 ORDER BY suppressed_at DESC
		 LIMIT 20
	`)
	if err != nil {
		return Deliverability{}, err
	}
	defer recent.Close()
	for recent.Next() {
		var s Suppression
		if err := recent.Scan(&s.Email, &s.Reason, &s.SuppressedAt); err != nil {
			return Deliverability{}, err
		}
		d.Recent = append(d.Recent, s)
	}
	return d, recent.Err()
}

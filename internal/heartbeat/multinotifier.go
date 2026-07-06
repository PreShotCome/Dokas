// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package heartbeat

import "context"

// MultiNotifier fans a monitor transition out to several Notifiers (e.g. email
// + push). Every notifier is invoked; the first error is returned but never
// short-circuits the rest, so one failing channel can't suppress the others.
type MultiNotifier []Notifier

func (m MultiNotifier) Notify(ctx context.Context, hb Heartbeat, event string) error {
	var firstErr error
	for _, n := range m {
		if n == nil {
			continue
		}
		if err := n.Notify(ctx, hb, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

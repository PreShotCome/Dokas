// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package apikey

import "testing"

func TestNormalizeScopes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty falls back to all", nil, AllScopes},
		{"all unknown falls back to all", []string{"x", "y"}, AllScopes},
		{"unknowns dropped", []string{"bogus", ScopeDrillsRead}, []string{ScopeDrillsRead}},
		{"account:delete is grantable when explicit",
			[]string{ScopeDrillsRead, ScopeAccountDelete},
			[]string{ScopeDrillsRead, ScopeAccountDelete}},
		{"dedup + canonical order",
			[]string{ScopeDrillsRead, ScopeDatabasesRead, ScopeDrillsRead},
			[]string{ScopeDatabasesRead, ScopeDrillsRead}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeScopes(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestHasScope(t *testing.T) {
	k := Key{Scopes: []string{ScopeDatabasesRead, ScopeDrillsRead}}
	if !k.HasScope(ScopeDatabasesRead) {
		t.Error("expected key to have databases:read")
	}
	if k.HasScope(ScopeDatabasesWrite) {
		t.Error("key should not have databases:write")
	}
}

func TestValidScope(t *testing.T) {
	for _, s := range AllScopes {
		if !ValidScope(s) {
			t.Errorf("ValidScope(%q) = false, want true", s)
		}
	}
	if ValidScope("databases:delete") {
		t.Error("ValidScope(databases:delete) = true, want false")
	}
	if !ValidScope(ScopeAccountDelete) {
		t.Error("ValidScope(account:delete) = false, want true")
	}
	// The destructive scope must be valid + grantable but never a default.
	for _, s := range AllScopes {
		if s == ScopeAccountDelete {
			t.Error("account:delete must not be in AllScopes (the default fallback set)")
		}
	}
}

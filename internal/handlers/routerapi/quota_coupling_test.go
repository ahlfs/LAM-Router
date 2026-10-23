package routerapi

import "testing"

func quotaByName(items []LiveModelQuotaItem, name string) (LiveModelQuotaItem, bool) {
	for _, q := range items {
		if q.Name == name {
			return q, true
		}
	}
	return LiveModelQuotaItem{}, false
}

// The weekly cap is the hard ceiling: a spent 7-day bucket means the 5-hour
// bucket cannot be used, whatever Google reports for it (a live account returns
// weekly 0.0 with a 5h fraction of 0.9992).
func TestApplyWeeklyExhaustionCoupling(t *testing.T) {
	t.Run("summary rows: spent weekly forces 5h to zero", func(t *testing.T) {
		got := applyWeeklyExhaustionCoupling([]LiveModelQuotaItem{
			{Name: "Claude and GPT models (7d)", PercentageValue: 0, Percentage: "0%", Status: "exhausted", Limit: 5000, Used: 5000, ResetIn: "1d 15h", ResetTime: "2026-10-25T09:04:53Z"},
			{Name: "Claude and GPT models (5h)", PercentageValue: 99, Percentage: "99%", Status: "ok", Limit: 1000, Used: 10, ResetIn: "3h 43m", ResetTime: "2026-10-23T13:50:58Z"},
		})

		fiveHour, ok := quotaByName(got, "Claude and GPT models (5h)")
		if !ok {
			t.Fatal("5h row missing")
		}
		if fiveHour.PercentageValue != 0 || fiveHour.Percentage != "0%" {
			t.Errorf("5h percentage = %d (%s), want 0 (0%%)", fiveHour.PercentageValue, fiveHour.Percentage)
		}
		if fiveHour.Status != "exhausted" {
			t.Errorf("5h status = %q, want exhausted", fiveHour.Status)
		}
		if !fiveHour.BlockedByWeekly {
			t.Error("5h row should be flagged BlockedByWeekly")
		}
		if fiveHour.Used != fiveHour.Limit {
			t.Errorf("5h used = %d, want %d (nothing left)", fiveHour.Used, fiveHour.Limit)
		}
		// The countdown must point at the weekly reset, not the 5h rollover.
		if fiveHour.ResetIn != "1d 15h" || fiveHour.ResetTime != "2026-10-25T09:04:53Z" {
			t.Errorf("5h reset = %q / %q, want the weekly reset", fiveHour.ResetIn, fiveHour.ResetTime)
		}
		if fiveHour.Note == "" {
			t.Error("5h row should carry a note explaining why it is empty")
		}
	})

	t.Run("estimate rows: same rule for the legacy naming", func(t *testing.T) {
		got := applyWeeklyExhaustionCoupling([]LiveModelQuotaItem{
			{Name: "Gemini Models · Weekly Limit Remaining", PercentageValue: 3, Percentage: "3%", Status: "exhausted", Limit: 2500000, ResetIn: "2d 4h"},
			{Name: "Gemini Models · Five Hour Limit Remaining", PercentageValue: 35, Percentage: "35%", Status: "ok", Limit: 500000, ResetIn: "2h 1m"},
		})

		fiveHour, ok := quotaByName(got, "Gemini Models · Five Hour Limit Remaining")
		if !ok {
			t.Fatal("5h row missing")
		}
		if fiveHour.PercentageValue != 0 || fiveHour.Status != "exhausted" || !fiveHour.BlockedByWeekly {
			t.Errorf("5h row not coupled: %+v", fiveHour)
		}
		if fiveHour.ResetIn != "2d 4h" {
			t.Errorf("5h reset = %q, want weekly reset 2d 4h", fiveHour.ResetIn)
		}
	})

	t.Run("other families are untouched", func(t *testing.T) {
		got := applyWeeklyExhaustionCoupling([]LiveModelQuotaItem{
			{Name: "Claude and GPT models (7d)", PercentageValue: 0, Status: "exhausted", Limit: 5000},
			{Name: "Claude and GPT models (5h)", PercentageValue: 99, Status: "ok", Limit: 1000},
			{Name: "Gemini Models · Weekly Limit Remaining", PercentageValue: 80, Status: "ok"},
			{Name: "Gemini Models · Five Hour Limit Remaining", PercentageValue: 35, Status: "warning"},
		})

		gemini, _ := quotaByName(got, "Gemini Models · Five Hour Limit Remaining")
		if gemini.PercentageValue != 35 || gemini.Status != "warning" || gemini.BlockedByWeekly {
			t.Errorf("gemini 5h should stay untouched, got %+v", gemini)
		}
		if gemini.ResetIn != "" {
			t.Errorf("gemini 5h reset should not be rewritten, got %q", gemini.ResetIn)
		}
	})

	t.Run("weekly still has room: 5h stays as reported", func(t *testing.T) {
		got := applyWeeklyExhaustionCoupling([]LiveModelQuotaItem{
			{Name: "Claude and GPT models (7d)", PercentageValue: 40, Status: "ok"},
			{Name: "Claude and GPT models (5h)", PercentageValue: 12, Status: "warning"},
		})
		fiveHour, _ := quotaByName(got, "Claude and GPT models (5h)")
		if fiveHour.PercentageValue != 12 || fiveHour.BlockedByWeekly {
			t.Errorf("5h row should be unchanged, got %+v", fiveHour)
		}
	})

	t.Run("no weekly row is not treated as exhausted", func(t *testing.T) {
		got := applyWeeklyExhaustionCoupling([]LiveModelQuotaItem{
			{Name: "Claude and GPT models (5h)", PercentageValue: 50, Status: "ok"},
		})
		if got[0].PercentageValue != 50 || got[0].BlockedByWeekly {
			t.Errorf("5h row without a weekly sibling must not be zeroed: %+v", got[0])
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := applyWeeklyExhaustionCoupling(nil); len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})
}

func TestQuotaFamilyAndWindow(t *testing.T) {
	cases := []struct{ name, family, window string }{
		{"Claude and GPT models (5h)", "Claude and GPT models", "5h"},
		{"Claude and GPT models (7d)", "Claude and GPT models", "7d"},
		{"Gemini Models · Weekly Limit Remaining", "Gemini Models", "7d"},
		{"Gemini Models · Five Hour Limit Remaining", "Gemini Models", "5h"},
		{"Gemini Models · 5-Day Limit Remaining", "Gemini Models", "7d"},
		{"Something else", "Something else", ""},
	}
	for _, c := range cases {
		if got := quotaFamilyOf(c.name); got != c.family {
			t.Errorf("quotaFamilyOf(%q) = %q, want %q", c.name, got, c.family)
		}
		if got := quotaWindowOf(c.name); got != c.window {
			t.Errorf("quotaWindowOf(%q) = %q, want %q", c.name, got, c.window)
		}
	}
}

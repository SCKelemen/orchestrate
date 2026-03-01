package schedule

import (
	"testing"
	"time"
)

// --- Cron parsing ---

func TestParseCronValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		expr string
	}{
		{"0 0 * * *"},
		{"*/15 * * * *"},
		{"1-5 9-17 * * 1-5"},
		{"30 2 1 1,6 *"},
		{"0 0 1 1 0"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			c, err := ParseCron(tc.expr)
			if err != nil {
				t.Fatalf("ParseCron(%q) error: %v", tc.expr, err)
			}
			if c == nil {
				t.Fatalf("ParseCron(%q) returned nil", tc.expr)
			}
		})
	}
}

func TestParseCronInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"0 0 * *",          // 4 fields
		"0 0 * * * *",      // 6 fields
		"60 0 * * *",       // minute out of range
		"0 24 * * *",       // hour out of range
		"0 0 32 * *",       // day out of range
		"0 0 * 13 *",       // month out of range
		"0 0 * * 7",        // dow out of range
		"*/0 * * * *",      // step 0
		"abc * * * *",      // non-numeric
	}
	for _, expr := range cases {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCron(expr)
			if err == nil {
				t.Fatalf("ParseCron(%q) expected error, got nil", expr)
			}
		})
	}
}

func TestCronNext(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC) // Sunday

	cases := []struct {
		name string
		expr string
		want time.Time
	}{
		{
			name: "hourly",
			expr: "0 * * * *",
			want: time.Date(2025, 6, 15, 11, 0, 0, 0, time.UTC),
		},
		{
			name: "daily at midnight",
			expr: "0 0 * * *",
			want: time.Date(2025, 6, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "weekday only (Mon-Fri), base is Sunday",
			expr: "0 9 * * 1-5",
			want: time.Date(2025, 6, 16, 9, 0, 0, 0, time.UTC), // Monday
		},
		{
			name: "every 15 minutes",
			expr: "*/15 * * * *",
			want: time.Date(2025, 6, 15, 10, 45, 0, 0, time.UTC),
		},
		{
			name: "month boundary",
			expr: "0 0 1 * *",
			want: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := ParseCron(tc.expr)
			if err != nil {
				t.Fatalf("ParseCron(%q): %v", tc.expr, err)
			}
			got := c.Next(base)
			if !got.Equal(tc.want) {
				t.Errorf("Next(%v) = %v, want %v", base, got, tc.want)
			}
		})
	}
}

// --- ISO 8601 Duration ---

func TestParseISO8601DurationValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  Duration
	}{
		{"PT1H", Duration{Hours: 1}},
		{"P1D", Duration{Days: 1}},
		{"P1Y6M", Duration{Years: 1, Months: 6}},
		{"P2W", Duration{Weeks: 2}},
		{"PT30M", Duration{Minutes: 30}},
		{"PT1H30M", Duration{Hours: 1, Minutes: 30}},
		{"P1DT12H", Duration{Days: 1, Hours: 12}},
		{"PT45S", Duration{Seconds: 45}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			d, err := ParseISO8601Duration(tc.input)
			if err != nil {
				t.Fatalf("ParseISO8601Duration(%q): %v", tc.input, err)
			}
			if *d != tc.want {
				t.Errorf("got %+v, want %+v", *d, tc.want)
			}
		})
	}
}

func TestParseISO8601DurationInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"P",
		"hello",
		"",
		"PT",
		"1H",
		"PX",
	}
	for _, s := range cases {
		s := s
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, err := ParseISO8601Duration(s)
			if err == nil {
				t.Fatalf("ParseISO8601Duration(%q) expected error", s)
			}
		})
	}
}

func TestDurationAddTo(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		dur  Duration
		want time.Time
	}{
		{
			name: "add 1 hour",
			dur:  Duration{Hours: 1},
			want: time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
		},
		{
			name: "add 1 day",
			dur:  Duration{Days: 1},
			want: time.Date(2025, 1, 16, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "month boundary: Jan 31 + 1 month",
			dur:  Duration{Months: 1},
			want: time.Date(2025, 2, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "add 2 weeks",
			dur:  Duration{Weeks: 2},
			want: time.Date(2025, 1, 29, 10, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.dur.AddTo(base)
			if !got.Equal(tc.want) {
				t.Errorf("AddTo(%v) = %v, want %v", base, got, tc.want)
			}
		})
	}
}

func TestDurationStringRoundtrip(t *testing.T) {
	t.Parallel()
	cases := []Duration{
		{Hours: 1},
		{Days: 1},
		{Years: 1, Months: 6},
		{Weeks: 2},
		{Days: 1, Hours: 12},
	}
	for _, d := range cases {
		d := d
		t.Run(d.String(), func(t *testing.T) {
			t.Parallel()
			s := d.String()
			parsed, err := ParseISO8601Duration(s)
			if err != nil {
				t.Fatalf("roundtrip parse(%q): %v", s, err)
			}
			if *parsed != d {
				t.Errorf("roundtrip: got %+v, want %+v", *parsed, d)
			}
		})
	}
}

// --- ISO 8601 Interval ---

func TestParseISO8601Interval(t *testing.T) {
	t.Parallel()

	t.Run("infinite repeat", func(t *testing.T) {
		t.Parallel()
		count, start, dur, err := ParseISO8601Interval("R/PT1H")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if count != -1 {
			t.Errorf("count = %d, want -1", count)
		}
		if start != nil {
			t.Errorf("start = %v, want nil", start)
		}
		if dur.Hours != 1 {
			t.Errorf("dur.Hours = %d, want 1", dur.Hours)
		}
	})

	t.Run("finite repeat", func(t *testing.T) {
		t.Parallel()
		count, start, dur, err := ParseISO8601Interval("R5/PT30M")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if count != 5 {
			t.Errorf("count = %d, want 5", count)
		}
		if start != nil {
			t.Errorf("start = %v, want nil", start)
		}
		if dur.Minutes != 30 {
			t.Errorf("dur.Minutes = %d, want 30", dur.Minutes)
		}
	})

	t.Run("with start time", func(t *testing.T) {
		t.Parallel()
		count, start, dur, err := ParseISO8601Interval("R3/2025-01-01T00:00:00Z/P1D")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if count != 3 {
			t.Errorf("count = %d, want 3", count)
		}
		if start == nil {
			t.Fatalf("start is nil")
		}
		want := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		if !start.Equal(want) {
			t.Errorf("start = %v, want %v", *start, want)
		}
		if dur.Days != 1 {
			t.Errorf("dur.Days = %d, want 1", dur.Days)
		}
	})
}

// --- Parse dispatch ---

func TestParseDispatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		expr     string
		wantType Type
	}{
		{"cron", "0 0 * * *", TypeCron},
		{"iso duration", "PT1H", TypeInterval},
		{"iso interval", "R/PT1H", TypeInterval},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec, err := Parse(tc.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.expr, err)
			}
			if spec.Type != tc.wantType {
				t.Errorf("type = %q, want %q", spec.Type, tc.wantType)
			}
			if spec.Raw != tc.expr {
				t.Errorf("raw = %q, want %q", spec.Raw, tc.expr)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"not a schedule",
		"0 0 * *",
	}
	for _, expr := range cases {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(expr)
			if err == nil {
				t.Fatalf("Parse(%q) expected error", expr)
			}
		})
	}
}

func TestSpecNext(t *testing.T) {
	t.Parallel()
	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	t.Run("cron spec", func(t *testing.T) {
		t.Parallel()
		spec, err := Parse("0 12 * * *")
		if err != nil {
			t.Fatal(err)
		}
		got := spec.Next(base)
		want := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("interval spec", func(t *testing.T) {
		t.Parallel()
		spec, err := Parse("PT2H")
		if err != nil {
			t.Fatal(err)
		}
		got := spec.Next(base)
		want := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

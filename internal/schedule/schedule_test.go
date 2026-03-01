package schedule

import (
	"testing"
	"time"
)

func TestParseCronSpecAndNext(t *testing.T) {
	t.Parallel()

	spec, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Type != TypeCron {
		t.Fatalf("type=%s want=%s", spec.Type, TypeCron)
	}

	base := time.Date(2026, 3, 1, 10, 7, 0, 0, time.UTC)
	next := spec.Next(base)
	want := time.Date(2026, 3, 1, 10, 15, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}

func TestParseISO8601DurationSpecAndNext(t *testing.T) {
	t.Parallel()

	spec, err := Parse("PT30M")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Type != TypeInterval {
		t.Fatalf("type=%s want=%s", spec.Type, TypeInterval)
	}

	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	next := spec.Next(base)
	want := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}

func TestParseISO8601Interval(t *testing.T) {
	t.Parallel()

	repeat, start, dur, err := ParseISO8601Interval("R5/2026-03-01T00:00:00Z/PT1H")
	if err != nil {
		t.Fatalf("parse interval: %v", err)
	}
	if repeat != 5 {
		t.Fatalf("repeat=%d want=5", repeat)
	}
	if start == nil {
		t.Fatal("start=nil")
	}
	if dur == nil || dur.Hours != 1 {
		t.Fatalf("duration=%+v want 1h", dur)
	}
}

func TestParseInvalidSchedule(t *testing.T) {
	t.Parallel()

	if _, err := Parse("not-a-schedule"); err == nil {
		t.Fatal("expected parse error")
	}
}

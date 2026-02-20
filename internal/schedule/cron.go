package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronExpr represents a parsed cron expression.
// Supports standard 5-field cron: minute hour day-of-month month day-of-week
type CronExpr struct {
	Minutes    []int // 0-59
	Hours      []int // 0-23
	DaysOfMonth []int // 1-31
	Months     []int // 1-12
	DaysOfWeek []int // 0-6 (Sunday=0)
}

// ParseCron parses a standard 5-field cron expression.
// Format: minute hour day-of-month month day-of-week
// Supports: *, ranges (1-5), steps (*/5), lists (1,3,5)
func ParseCron(expr string) (*CronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	return &CronExpr{
		Minutes:     minutes,
		Hours:       hours,
		DaysOfMonth: dom,
		Months:      months,
		DaysOfWeek:  dow,
	}, nil
}

// Next returns the next time after t that matches the cron expression.
func (c *CronExpr) Next(t time.Time) time.Time {
	// Start from the next minute
	t = t.Truncate(time.Minute).Add(time.Minute)

	// Search up to 4 years ahead (handles leap year edge cases)
	limit := t.Add(4 * 365 * 24 * time.Hour)

	for t.Before(limit) {
		if !contains(c.Months, int(t.Month())) {
			// Advance to next matching month
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(c.DaysOfMonth, t.Day()) || !contains(c.DaysOfWeek, int(t.Weekday())) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(c.Hours, t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if !contains(c.Minutes, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}

	// Should not happen for valid expressions
	return time.Time{}
}

func parseField(field string, min, max int) ([]int, error) {
	var result []int
	for _, part := range strings.Split(field, ",") {
		vals, err := parsePart(part, min, max)
		if err != nil {
			return nil, err
		}
		result = append(result, vals...)
	}
	return dedupSort(result, min, max), nil
}

func parsePart(part string, min, max int) ([]int, error) {
	// Handle step: */2 or 1-10/2
	step := 1
	if i := strings.Index(part, "/"); i >= 0 {
		s, err := strconv.Atoi(part[i+1:])
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step: %s", part)
		}
		step = s
		part = part[:i]
	}

	// Handle wildcard
	if part == "*" {
		var vals []int
		for i := min; i <= max; i += step {
			vals = append(vals, i)
		}
		return vals, nil
	}

	// Handle range: 1-5
	if i := strings.Index(part, "-"); i >= 0 {
		lo, err := strconv.Atoi(part[:i])
		if err != nil {
			return nil, fmt.Errorf("invalid range start: %s", part)
		}
		hi, err := strconv.Atoi(part[i+1:])
		if err != nil {
			return nil, fmt.Errorf("invalid range end: %s", part)
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("range out of bounds: %d-%d (valid: %d-%d)", lo, hi, min, max)
		}
		var vals []int
		for v := lo; v <= hi; v += step {
			vals = append(vals, v)
		}
		return vals, nil
	}

	// Single value
	v, err := strconv.Atoi(part)
	if err != nil {
		return nil, fmt.Errorf("invalid value: %s", part)
	}
	if v < min || v > max {
		return nil, fmt.Errorf("value %d out of range %d-%d", v, min, max)
	}
	return []int{v}, nil
}

func dedupSort(vals []int, min, max int) []int {
	seen := make([]bool, max-min+1)
	for _, v := range vals {
		if v >= min && v <= max {
			seen[v-min] = true
		}
	}
	var result []int
	for i, s := range seen {
		if s {
			result = append(result, i+min)
		}
	}
	return result
}

func contains(vals []int, v int) bool {
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}

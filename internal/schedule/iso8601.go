package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Duration represents an ISO 8601 duration.
// See: https://en.wikipedia.org/wiki/ISO_8601#Durations
type Duration struct {
	Years   int
	Months  int
	Weeks   int
	Days    int
	Hours   int
	Minutes int
	Seconds int
}

// ParseISO8601Duration parses an ISO 8601 duration string.
// Format: P[nY][nM][nW][nD][T[nH][nM][nS]]
// Examples: PT1H, P1D, P1Y6M, PT30M, P2W
func ParseISO8601Duration(s string) (*Duration, error) {
	if len(s) < 2 || s[0] != 'P' {
		return nil, fmt.Errorf("ISO 8601 duration must start with 'P': %s", s)
	}

	d := &Duration{}
	s = s[1:] // strip leading P

	inTime := false
	for len(s) > 0 {
		if s[0] == 'T' {
			inTime = true
			s = s[1:]
			continue
		}

		// Read the numeric part
		numEnd := 0
		for numEnd < len(s) && (unicode.IsDigit(rune(s[numEnd])) || s[numEnd] == '.') {
			numEnd++
		}
		if numEnd == 0 || numEnd >= len(s) {
			return nil, fmt.Errorf("invalid ISO 8601 duration: unexpected token at %q", s)
		}

		n, err := strconv.Atoi(s[:numEnd])
		if err != nil {
			return nil, fmt.Errorf("invalid number in duration: %s", s[:numEnd])
		}

		designator := s[numEnd]
		s = s[numEnd+1:]

		if inTime {
			switch designator {
			case 'H':
				d.Hours = n
			case 'M':
				d.Minutes = n
			case 'S':
				d.Seconds = n
			default:
				return nil, fmt.Errorf("invalid time designator: %c", designator)
			}
		} else {
			switch designator {
			case 'Y':
				d.Years = n
			case 'M':
				d.Months = n
			case 'W':
				d.Weeks = n
			case 'D':
				d.Days = n
			default:
				return nil, fmt.Errorf("invalid date designator: %c", designator)
			}
		}
	}

	if d.IsZero() {
		return nil, fmt.Errorf("empty duration: no components found")
	}

	return d, nil
}

// IsZero returns true if the duration has no components.
func (d *Duration) IsZero() bool {
	return d.Years == 0 && d.Months == 0 && d.Weeks == 0 && d.Days == 0 &&
		d.Hours == 0 && d.Minutes == 0 && d.Seconds == 0
}

// AddTo adds the duration to the given time.
func (d *Duration) AddTo(t time.Time) time.Time {
	t = t.AddDate(d.Years, d.Months, d.Weeks*7+d.Days)
	t = t.Add(time.Duration(d.Hours)*time.Hour +
		time.Duration(d.Minutes)*time.Minute +
		time.Duration(d.Seconds)*time.Second)
	return t
}

// String returns the ISO 8601 representation.
func (d *Duration) String() string {
	var b strings.Builder
	b.WriteByte('P')

	if d.Years > 0 {
		fmt.Fprintf(&b, "%dY", d.Years)
	}
	if d.Months > 0 {
		fmt.Fprintf(&b, "%dM", d.Months)
	}
	if d.Weeks > 0 {
		fmt.Fprintf(&b, "%dW", d.Weeks)
	}
	if d.Days > 0 {
		fmt.Fprintf(&b, "%dD", d.Days)
	}

	if d.Hours > 0 || d.Minutes > 0 || d.Seconds > 0 {
		b.WriteByte('T')
		if d.Hours > 0 {
			fmt.Fprintf(&b, "%dH", d.Hours)
		}
		if d.Minutes > 0 {
			fmt.Fprintf(&b, "%dM", d.Minutes)
		}
		if d.Seconds > 0 {
			fmt.Fprintf(&b, "%dS", d.Seconds)
		}
	}

	return b.String()
}

// ParseISO8601Interval parses an ISO 8601 repeating interval.
// Format: R[n]/[start]/duration or R[n]/duration
// R5/PT1H means repeat 5 times every hour
// R/P1D means repeat forever every day
func ParseISO8601Interval(s string) (repeatCount int, start *time.Time, dur *Duration, err error) {
	if !strings.HasPrefix(s, "R") {
		return 0, nil, nil, fmt.Errorf("repeating interval must start with 'R': %s", s)
	}

	parts := strings.SplitN(s, "/", 3)

	// Parse repeat count (R or R5)
	rPart := parts[0][1:] // strip R
	if rPart == "" {
		repeatCount = -1 // infinite
	} else {
		n, err := strconv.Atoi(rPart)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("invalid repeat count: %s", rPart)
		}
		repeatCount = n
	}

	switch len(parts) {
	case 2:
		// R[n]/duration
		dur, err = ParseISO8601Duration(parts[1])
		if err != nil {
			return 0, nil, nil, err
		}
	case 3:
		// R[n]/start/duration
		t, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			return 0, nil, nil, fmt.Errorf("invalid start time: %w", err)
		}
		start = &t
		dur, err = ParseISO8601Duration(parts[2])
		if err != nil {
			return 0, nil, nil, err
		}
	default:
		return 0, nil, nil, fmt.Errorf("invalid interval format: %s", s)
	}

	return repeatCount, start, dur, nil
}

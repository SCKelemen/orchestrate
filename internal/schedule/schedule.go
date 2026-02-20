// Package schedule provides cron and ISO 8601 duration-based scheduling.
package schedule

import (
	"fmt"
	"time"
)

// Type identifies the scheduling mechanism.
type Type string

const (
	TypeCron     Type = "CRON"
	TypeInterval Type = "INTERVAL"
)

// Spec is a parsed schedule specification that can compute the next run time.
type Spec struct {
	Type     Type
	Raw      string
	Cron     *CronExpr
	Interval *Duration
}

// Parse parses a schedule expression, detecting cron or ISO 8601 format.
// Cron: "30 * * * *" (5-field)
// ISO 8601 duration: "PT1H", "P1D", "P2W"
// ISO 8601 repeating interval: "R/PT1H", "R5/PT30M"
func Parse(expr string) (*Spec, error) {
	// ISO 8601 repeating interval
	if len(expr) > 0 && expr[0] == 'R' {
		_, _, dur, err := ParseISO8601Interval(expr)
		if err != nil {
			return nil, err
		}
		return &Spec{
			Type:     TypeInterval,
			Raw:      expr,
			Interval: dur,
		}, nil
	}

	// ISO 8601 duration
	if len(expr) > 0 && expr[0] == 'P' {
		dur, err := ParseISO8601Duration(expr)
		if err != nil {
			return nil, err
		}
		return &Spec{
			Type:     TypeInterval,
			Raw:      expr,
			Interval: dur,
		}, nil
	}

	// Cron expression
	cron, err := ParseCron(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule expression: %w", err)
	}
	return &Spec{
		Type: TypeCron,
		Raw:  expr,
		Cron: cron,
	}, nil
}

// Next returns the next occurrence after t.
func (s *Spec) Next(t time.Time) time.Time {
	switch s.Type {
	case TypeCron:
		return s.Cron.Next(t)
	case TypeInterval:
		return s.Interval.AddTo(t)
	default:
		return time.Time{}
	}
}

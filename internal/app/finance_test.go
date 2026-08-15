package app

import (
	"testing"
	"time"
)

func TestFinancePeriodUsesSalaryDay(t *testing.T) {
	location := time.FixedZone("KST", 9*60*60)
	tests := []struct {
		name string
		now  time.Time
		from string
		to   string
	}{
		{name: "before salary day", now: time.Date(2026, time.August, 20, 12, 0, 0, 0, location), from: "2026-07-21", to: "2026-08-20"},
		{name: "on salary day", now: time.Date(2026, time.August, 21, 12, 0, 0, 0, location), from: "2026-08-21", to: "2026-09-20"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			from, to := financePeriod(test.now, 21)
			if from != test.from || to != test.to {
				t.Fatalf("financePeriod() = %s..%s, want %s..%s", from, to, test.from, test.to)
			}
		})
	}
}

func TestFinancePeriodOffset(t *testing.T) {
	tests := []struct {
		value   string
		want    int
		wantErr bool
	}{
		{value: "", want: 0},
		{value: "-12", want: -12},
		{value: "12", want: 12},
		{value: "13", wantErr: true},
		{value: "-121", wantErr: true},
		{value: "last", wantErr: true},
	}
	for _, test := range tests {
		got, err := financePeriodOffset(test.value)
		if (err != nil) != test.wantErr || got != test.want {
			t.Errorf("financePeriodOffset(%q) = %d, %v; want %d, error=%v", test.value, got, err, test.want, test.wantErr)
		}
	}
}

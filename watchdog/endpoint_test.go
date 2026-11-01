package watchdog

import (
	"testing"
	"time"
)

func TestNextAlignedRun(t *testing.T) {
	scenarios := []struct {
		name     string
		t        string
		interval time.Duration
		offset   time.Duration
		expected string
	}{
		{
			name:     "between-slots",
			t:        "2026-11-01T03:01:51Z",
			interval: 5 * time.Minute,
			expected: "2026-11-01T03:05:00Z",
		},
		{
			name:     "on-a-slot-returns-the-next-one",
			t:        "2026-11-01T03:05:00Z",
			interval: 5 * time.Minute,
			expected: "2026-11-01T03:10:00Z",
		},
		{
			name:     "hourly",
			t:        "2026-11-01T03:59:59.999Z",
			interval: time.Hour,
			expected: "2026-11-01T04:00:00Z",
		},
		{
			name:     "offset-past-the-slot",
			t:        "2026-11-01T03:01:51Z",
			interval: 5 * time.Minute,
			offset:   444 * time.Millisecond,
			expected: "2026-11-01T03:05:00.444Z",
		},
		{
			name:     "after-the-slot-but-before-its-offset",
			t:        "2026-11-01T03:05:00.2Z",
			interval: 5 * time.Minute,
			offset:   444 * time.Millisecond,
			expected: "2026-11-01T03:05:00.444Z",
		},
		{
			name:     "on-an-offset-slot-returns-the-next-one",
			t:        "2026-11-01T03:05:00.444Z",
			interval: 5 * time.Minute,
			offset:   444 * time.Millisecond,
			expected: "2026-11-01T03:10:00.444Z",
		},
		{
			name:     "offset-longer-than-interval",
			t:        "2026-11-01T03:00:10Z",
			interval: time.Minute,
			offset:   90 * time.Second,
			expected: "2026-11-01T03:00:30Z",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339Nano, scenario.t)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := time.Parse(time.RFC3339Nano, scenario.expected)
			if err != nil {
				t.Fatal(err)
			}
			if actual := nextAlignedRun(now, scenario.interval, scenario.offset); !actual.Equal(expected) {
				t.Errorf("expected %s, got %s", expected.Format(time.RFC3339Nano), actual.UTC().Format(time.RFC3339Nano))
			}
		})
	}
}

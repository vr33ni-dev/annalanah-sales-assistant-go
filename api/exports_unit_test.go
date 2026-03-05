package api

import (
	"testing"
	"time"
)

func TestParseMonthParam(t *testing.T) {
	if _, err := parseMonthParam("2026-02"); err != nil {
		t.Fatalf("expected valid month, got err=%v", err)
	}
	if _, err := parseMonthParam(""); err != nil {
		t.Fatalf("expected empty month to be accepted, got err=%v", err)
	}
	if _, err := parseMonthParam("2026/02"); err == nil {
		t.Fatalf("expected invalid month error")
	}
}

func TestParseDateStringAndHelpers(t *testing.T) {
	if _, ok := parseDateString("2026-01-05"); !ok {
		t.Fatalf("expected date-only parse ok")
	}
	if _, ok := parseDateString("2026-01-05 10:20:30"); !ok {
		t.Fatalf("expected datetime parse ok")
	}
	if _, ok := parseDateString(time.Now().Format(time.RFC3339)); !ok {
		t.Fatalf("expected rfc3339 parse ok")
	}
	if _, ok := parseDateString("not-a-date"); ok {
		t.Fatalf("expected invalid date parse to fail")
	}

	if got := asString(nil); got != "" {
		t.Fatalf("expected empty string for nil, got %q", got)
	}
	if got := asString(42); got != "42" {
		t.Fatalf("expected fmt string conversion, got %q", got)
	}
}

func TestBuildMonthRangeInclusive_EndBeforeStart(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := buildMonthRangeInclusive(start, end); got != nil {
		t.Fatalf("expected nil range when end before start, got %v", got)
	}
}

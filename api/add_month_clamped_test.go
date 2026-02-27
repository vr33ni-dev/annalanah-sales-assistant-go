package api

import (
    "testing"
    "time"
)

func TestAddMonthClamped_EndOfMonth(t *testing.T) {
    d := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
    got := addMonthClamped(d, 1)
    want := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
    if !got.Equal(want) {
        t.Fatalf("expected %v, got %v", want, got)
    }
}

func TestAddMonthClamped_LeapYear(t *testing.T) {
    // 2024 is a leap year; moving 12 months to 2025 (non-leap) should clamp
    d := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
    got := addMonthClamped(d, 12)
    want := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
    if !got.Equal(want) {
        t.Fatalf("expected %v, got %v", want, got)
    }
}

func TestAddMonthClamped_PreserveDay(t *testing.T) {
    d := time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC)
    got := addMonthClamped(d, 1)
    want := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
    if !got.Equal(want) {
        t.Fatalf("expected %v, got %v", want, got)
    }
}

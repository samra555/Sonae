package models_test

import (
	"testing"

	"gotovnost/internal/models"
)

func TestNumStr(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{30, "30"},
		{-10, "-10"},
		{0.5, "0.5"},
		{1.0 / 3, "0.3333333333333333"},
	}
	for _, tc := range cases {
		if got := models.NumStr(tc.in); got != tc.want {
			t.Errorf("NumStr(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOptNum(t *testing.T) {
	if models.OptNum(nil) != "" {
		t.Errorf("OptNum(nil) = %q, want empty", models.OptNum(nil))
	}
	if models.OptNum(f(15)) != "15" {
		t.Errorf("OptNum(15) = %q, want 15", models.OptNum(f(15)))
	}
}

func TestPercentValue(t *testing.T) {
	if got := models.PercentValue(0.3); got != "30" {
		t.Errorf("PercentValue(0.3) = %q, want 30", got)
	}
	if got := models.PercentValue(0.155); got != "15.5" {
		t.Errorf("PercentValue(0.155) = %q, want 15.5", got)
	}
}

func TestParseNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"30", 30},
		{"30,5", 30.5},
		{"−10", -10},
		{"-10", -10},
		{"50%", 50},
		{" 12 ", 12},
	}
	for _, tc := range cases {
		got, err := models.ParseNumber(tc.in)
		if err != nil {
			t.Fatalf("ParseNumber(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseNumber(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseNumberInvalid(t *testing.T) {
	if _, err := models.ParseNumber("abc"); err == nil {
		t.Fatal("expected error for non-number")
	}
}

func TestParseOptionalNumber(t *testing.T) {
	got, err := models.ParseOptionalNumber("")
	if err != nil || got != nil {
		t.Fatalf("ParseOptionalNumber(\"\") = %v, %v; want nil, nil", got, err)
	}
	got, err = models.ParseOptionalNumber("42")
	if err != nil || got == nil || *got != 42 {
		t.Fatalf("ParseOptionalNumber(42) = %v, %v; want 42", got, err)
	}
}

func TestSameNumber(t *testing.T) {
	if !models.SameNumber(nil, nil) {
		t.Error("nil,nil should be same")
	}
	if models.SameNumber(nil, f(1)) || models.SameNumber(f(1), nil) {
		t.Error("nil vs value should not be same")
	}
	if !models.SameNumber(f(1.5), f(1.5)) {
		t.Error("1.5,1.5 should be same")
	}
	if models.SameNumber(f(1), f(2)) {
		t.Error("1,2 should not be same")
	}
}

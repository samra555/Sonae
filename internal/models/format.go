package models

import (
	"math"
	"strconv"
	"strings"
)

// NumStr uses the shortest exact representation, so a value shown in a form
// parses back to the identical number.
func NumStr(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

func OptNum(number *float64) string {
	if number == nil {
		return ""
	}
	return NumStr(*number)
}

func PercentValue(probability float64) string {
	return NumStr(math.Round(probability*100*10000) / 10000)
}

// ParseNumber accepts what people actually type: a comma, a typographic minus, a percent sign.
func ParseNumber(rawNumber string) (float64, error) {
	normalized := strings.NewReplacer(",", ".", "−", "-", "%", "", " ", "").Replace(rawNumber)
	return strconv.ParseFloat(normalized, 64)
}

func ParseOptionalNumber(rawNumber string) (*float64, error) {
	if strings.TrimSpace(rawNumber) == "" {
		return nil, nil
	}
	number, err := ParseNumber(rawNumber)
	if err != nil {
		return nil, err
	}
	return &number, nil
}

func SameNumber(first, second *float64) bool {
	if first == nil || second == nil {
		return first == second
	}
	return *first == *second
}

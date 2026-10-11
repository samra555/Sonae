package main

import (
	"math"
	"strings"
)

// An effect of -10 (percent) is stored as the multiplier 0.9, and +30 as 1.3.
const (
	minEffectPercent = (minMultiplier - 1) * 100
	maxEffectPercent = (maxMultiplier - 1) * 100
)

func validEffectPercent(effectPercent *float64) bool {
	if effectPercent == nil {
		return true
	}
	return *effectPercent >= minEffectPercent && *effectPercent <= maxEffectPercent
}

func multiplierFromPercent(effectPercent *float64) *float64 {
	if effectPercent == nil {
		return nil
	}
	multiplier := math.Round((1+*effectPercent/100)*1e6) / 1e6
	return &multiplier
}

func optionalPercent(multiplier *float64) *float64 {
	if multiplier == nil {
		return nil
	}
	effectPercent := math.Round((*multiplier-1)*100*1e4) / 1e4
	return &effectPercent
}

// pctLabel renders a percent for display: +30%, −10%, or a dash when there is no value.
func pctLabel(effectPercent *float64) string {
	if effectPercent == nil {
		return "—"
	}
	label := numStr(*effectPercent)
	if *effectPercent > 0 {
		label = "+" + label
	}
	return strings.Replace(label, "-", "−", 1) + "%"
}

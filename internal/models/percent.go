package models

import (
	"errors"
	"strings"
)

// An action's weight is a percent, but its meaning depends on the case:
//   - risk: percentage points of the probability; -10 turns 30% into 20%;
//   - aspiration: the share of the goal; the weights of all actions add up to 100.
const (
	minRiskWeight       = -100.0
	maxRiskWeight       = 100.0
	minAspirationWeight = 0.0
	maxAspirationWeight = 100.0
)

func weightRange(kind Kind) (float64, float64) {
	if kind == KindAspiration {
		return minAspirationWeight, maxAspirationWeight
	}
	return minRiskWeight, maxRiskWeight
}

// ValidWeight reports whether the weight is allowed for the case kind. A nil weight is always allowed.
func ValidWeight(kind Kind, weight *float64) bool {
	if weight == nil {
		return true
	}
	minWeight, maxWeight := weightRange(kind)
	return *weight >= minWeight && *weight <= maxWeight
}

// WeightError describes the allowed weight for the case kind.
func WeightError(kind Kind) error {
	if kind == KindAspiration {
		return errors.New("вес: число от 0 до 100, например 25")
	}
	return errors.New("вес: число от −100 до 100, например −10")
}

// PctLabel renders a percent for display: +30%, −10%, or a dash when there is no value.
func PctLabel(percent *float64) string {
	if percent == nil {
		return "—"
	}
	label := NumStr(*percent)
	if *percent > 0 {
		label = "+" + label
	}
	return strings.Replace(label, "-", "−", 1) + "%"
}

// WeightLabel shows a risk weight with its sign (it moves the probability up or down)
// and an aspiration weight without one (it is a share of the goal).
func WeightLabel(kind Kind, weight *float64) string {
	if weight == nil {
		return "—"
	}
	if kind == KindAspiration {
		return NumStr(*weight) + "%"
	}
	return PctLabel(weight)
}

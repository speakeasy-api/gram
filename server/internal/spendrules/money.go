package spendrules

import (
	"fmt"
	"math"
)

const centsPerUSD = 100

func USDToCents(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("invalid USD amount %v", amount)
	}
	maxUSD := float64(math.MaxInt64) / centsPerUSD
	if amount > maxUSD || amount < -maxUSD {
		return 0, fmt.Errorf("USD amount %v is outside cents range", amount)
	}
	return int64(math.Round(amount * centsPerUSD)), nil
}

func CentsToUSD(cents int64) float64 {
	return float64(cents) / centsPerUSD
}

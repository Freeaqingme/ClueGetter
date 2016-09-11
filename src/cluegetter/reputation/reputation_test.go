package reputation

import (
	"testing"
)

func TestIsAnomalous(t *testing.T) {
	series := []int{0, 1, 3, 5, 15, 5, 8, 7, 7, 3, 5, 9, 30, 6, 8, 14, 10, 13, 9, 11, 20, 14, 18, 0, 20, 0, 230}

	anomaly := isAnomalous(series, 0.1, 5)
	if !anomaly {
		t.Error("Series was not reported to be anomalous, but sohuld have been")
	}

	for i := 1; i < 10; i++ {
		seriesTrunc := series[:len(series)-i]
		anomaly = isAnomalous(seriesTrunc, 0.1, 5)
		if anomaly {
			t.Error("Series was reported to be anomalous, but sohuld not have been for series:", seriesTrunc)
		}
	}
}

func TestIsAnomalousEmptySeries(t *testing.T) {
	series := []int{}

	if isAnomalous(series, 0.1, 5) {
		t.Error("Series was reported to be anomalous, but sohuld not have been for empty series:")
	}
}

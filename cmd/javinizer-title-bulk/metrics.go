package main

import (
	"math"
	"sort"
)

func elapsedPercentiles(rows []resultRecord) (p50 int64, p95 int64) {
	values := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.ElapsedMS < 0 {
			continue
		}
		values = append(values, row.ElapsedMS)
	}
	return nearestRankPercentile(values, 0.50), nearestRankPercentile(values, 0.95)
}

func nearestRankPercentile(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	if percentile <= 0 {
		return ordered[0]
	}
	if percentile >= 1 {
		return ordered[len(ordered)-1]
	}
	rank := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(ordered) {
		rank = len(ordered) - 1
	}
	return ordered[rank]
}

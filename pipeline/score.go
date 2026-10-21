package main

import (
	"math"
	"sort"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

// Category order used everywhere in the output.
var catOrder = []string{"dirt", "smell", "graffiti", "incivism", "crime"}

var defaultWeights = map[string]float64{"crime": 0.35, "smell": 0.2, "dirt": 0.2, "graffiti": 0.15, "incivism": 0.1}

const (
	// Hexes with fewer residents still get values, but are left out of the
	// reference distribution and flagged: a per-resident rate there is noise.
	residentialMinPop = 100
	// Empirical-Bayes style shrinkage toward the city rate, in residents.
	hexPriorPop   = 1000
	barriPriorPop = 2000
	// Weight of each of the six neighbouring hexes when smoothing.
	ringWeight = 0.5
)

// hexRates returns, per hex, complaints per resident for one IRIS category,
// smoothed over the hex and its first ring and shrunk toward the city rate.
func hexRates(hexes map[h3.Cell]*Hex, cat int) map[h3.Cell]float64 {
	var totC, totP float64
	for _, h := range hexes {
		totC += h.Counts[cat]
		totP += h.Pop
	}
	city := totC / totP
	out := make(map[h3.Cell]float64, len(hexes))
	for c, h := range hexes {
		cs, ps := h.Counts[cat], h.Pop
		disk, err := h3.GridDisk(c, 1)
		if err == nil {
			for _, n := range disk {
				if nh := hexes[n]; nh != nil && n != c {
					cs += ringWeight * nh.Counts[cat]
					ps += ringWeight * nh.Pop
				}
			}
		}
		out[c] = (cs + hexPriorPop*city) / (ps + hexPriorPop)
	}
	return out
}

// barriCrimeRates returns crimes per resident per barri, shrunk toward the city rate.
func barriCrimeRates(crime map[int]float64, pop map[int]float64) map[int]float64 {
	var totC, totP float64
	for b, c := range crime {
		totC += c
		totP += pop[b]
	}
	city := totC / totP
	out := map[int]float64{}
	for b, c := range crime {
		out[b] = (c + barriPriorPop*city) / (pop[b] + barriPriorPop)
	}
	return out
}

// percentileOf gives the mid-rank percentile (0-100) of v within sorted ref.
func percentileOf(ref []float64, v float64) float64 {
	lo := sort.SearchFloat64s(ref, v)
	hi := sort.Search(len(ref), func(i int) bool { return ref[i] > v })
	return 100 * (float64(lo) + 0.5*float64(hi-lo)) / float64(len(ref))
}

// percentiles converts per-hex values into city-wide percentiles, using
// residential hexes as the reference distribution.
func percentiles(hexes map[h3.Cell]*Hex, vals map[h3.Cell]float64) map[h3.Cell]float64 {
	var ref []float64
	for c, h := range hexes {
		if h.Pop >= residentialMinPop {
			ref = append(ref, vals[c])
		}
	}
	sort.Float64s(ref)
	out := make(map[h3.Cell]float64, len(vals))
	for c, v := range vals {
		out[c] = percentileOf(ref, v)
	}
	return out
}

func score(pct map[string]float64, w map[string]float64) float64 {
	var s, tw float64
	for _, k := range catOrder {
		s += w[k] * pct[k]
		tw += w[k]
	}
	return 100 - s/tw
}

func round(v float64) int { return int(math.Round(v)) }

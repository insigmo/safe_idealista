package main

import (
	"fmt"
	"strconv"
	"strings"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

// month is year*12 + (month-1), so windows are simple integer ranges.
type month int

func mkMonth(y, m int) month   { return month(y*12 + m - 1) }
func (m month) String() string { return fmt.Sprintf("%04d-%02d", int(m)/12, int(m)%12+1) }

type irisRecord struct {
	Month                 month
	Lat, Lon              float64
	Area, Element, Detail string
}

// Complaint-type records only: incidents and complaints (the 2025 file labels
// some of them in English). Queries, suggestions, service requests and thanks
// are not evidence of a problem on the street.
var irisTypes = map[string]bool{"INCIDENCIA": true, "QUEIXA": true, "ISSUE": true, "COMPLAINT": true}

type irisStats struct {
	Rows, Duplicates, WrongType, NoCoords, OutsideBCN int
	ByMonth                                           map[month]int
}

// loadIRIS reads the yearly files, which are split by closing date. A request
// is one FITXA_ID: the files repeat some rows verbatim (8,914 in 2024–2026),
// and those copies are dropped so no complaint is counted twice.
func loadIRIS(paths []string) ([]irisRecord, irisStats, error) {
	st := irisStats{ByMonth: map[month]int{}}
	var out []irisRecord
	seen := map[string]bool{}
	for _, p := range paths {
		idx, rows, err := readCSV(p)
		if err != nil {
			return nil, st, err
		}
		c, err := col(idx, p, "TIPUS", "AREA", "ELEMENT", "DETALL", "MES_DATA_ALTA", "ANY_DATA_ALTA", "LATITUD", "LONGITUD", "FITXA_ID")
		if err != nil {
			return nil, st, err
		}
		for _, r := range rows {
			st.Rows++
			if id := strings.TrimSpace(r[c[8]]); id != "" {
				if seen[id] {
					st.Duplicates++
					continue
				}
				seen[id] = true
			}
			if !irisTypes[strings.ToUpper(foldAccents.Replace(strings.ToLower(strings.TrimSpace(r[c[0]]))))] {
				st.WrongType++
				continue
			}
			lat, err1 := strconv.ParseFloat(strings.TrimSpace(r[c[6]]), 64)
			lon, err2 := strconv.ParseFloat(strings.TrimSpace(r[c[7]]), 64)
			if err1 != nil || err2 != nil {
				st.NoCoords++
				continue
			}
			if !inBarcelona(lat, lon) {
				st.OutsideBCN++
				continue
			}
			m := mkMonth(atoi(r[c[5]]), atoi(r[c[4]]))
			st.ByMonth[m]++
			out = append(out, irisRecord{Month: m, Lat: lat, Lon: lon, Area: r[c[1]], Element: r[c[2]], Detail: r[c[3]]})
		}
	}
	return out, st, nil
}

// countIRIS adds every mapped record opened in [from, to] (both inclusive) to
// the hex its coordinates fall in. It returns the tally per AREA/ELEMENT/DETALL,
// the per-category totals and how many mapped records fell in no hex.
func countIRIS(recs []irisRecord, from, to month, hexes map[h3.Cell]*Hex, res int) (map[[3]string]*mapRow, map[string]int, int, error) {
	mapping := map[[3]string]*mapRow{}
	totals := map[string]int{}
	var outside int
	for _, r := range recs {
		if r.Month < from || r.Month > to {
			continue
		}
		k := [3]string{r.Area, r.Element, r.Detail}
		mr := mapping[k]
		if mr == nil {
			mr = &mapRow{Area: r.Area, Element: r.Element, Detail: r.Detail, Cat: classify(r.Area, r.Element, r.Detail)}
			mapping[k] = mr
		}
		mr.N++
		if mr.Cat == catNone {
			continue
		}
		cell, err := h3.LatLngToCell(h3.NewLatLng(r.Lat, r.Lon), res)
		if err != nil {
			return nil, nil, 0, err
		}
		h := hexes[cell]
		if h == nil {
			outside++ // geocoded to sea, port or beyond the municipal boundary
			continue
		}
		h.Counts[mr.Cat]++
		totals[irisCatNames[mr.Cat]]++
	}
	return mapping, totals, outside, nil
}

// latestFullMonth is the newest month with at least minRecords records; a few
// stray records with later dates should not move the window.
func latestFullMonth(byMonth map[month]int, minRecords int) month {
	var best month
	for m, n := range byMonth {
		if n >= minRecords && m > best {
			best = m
		}
	}
	return best
}

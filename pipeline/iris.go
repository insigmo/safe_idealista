package main

import (
	"fmt"
	"strconv"
	"strings"
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
	Rows, WrongType, NoCoords, OutsideBCN int
	ByMonth                               map[month]int
}

func loadIRIS(paths []string) ([]irisRecord, irisStats, error) {
	st := irisStats{ByMonth: map[month]int{}}
	var out []irisRecord
	for _, p := range paths {
		idx, rows, err := readCSV(p)
		if err != nil {
			return nil, st, err
		}
		c, err := col(idx, p, "TIPUS", "AREA", "ELEMENT", "DETALL", "MES_DATA_ALTA", "ANY_DATA_ALTA", "LATITUD", "LONGITUD")
		if err != nil {
			return nil, st, err
		}
		for _, r := range rows {
			st.Rows++
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

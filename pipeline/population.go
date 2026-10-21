package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

// Barri is one of Barcelona's 73 neighbourhoods.
type Barri struct {
	Code         int
	Name         string
	DistrictCode int
	District     string
}

// Section is a census section ("secció censal"), keyed by district*1000+section.
type Section struct {
	Key    int
	Barri  int
	Shape  *Shape
	Pop    float64
	Income float64 // € per person, 0 when unknown
}

// Hex accumulates everything known about one H3 cell.
type Hex struct {
	Cell      h3.Cell
	Pop       float64
	incomePop float64 // population with a known income, for the weighted mean
	incomeSum float64
	barriPop  map[int]float64
	samples   int
	Counts    [nIris]float64
	Barri     int
}

func (h *Hex) Income() float64 {
	if h.incomePop == 0 {
		return 0
	}
	return h.incomeSum / h.incomePop
}

// readCSV returns the header index and all rows of a CSV file, stripping a UTF-8 BOM.
func readCSV(path string) (map[string]int, [][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimPrefix(strings.TrimSpace(h), "\uFEFF")] = i
	}
	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		rows = append(rows, rec)
	}
	return idx, rows, nil
}

func col(idx map[string]int, path string, names ...string) ([]int, error) {
	out := make([]int, len(names))
	for i, n := range names {
		c, ok := idx[n]
		if !ok {
			return nil, fmt.Errorf("%s: missing column %q", path, n)
		}
		out[i] = c
	}
	return out, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func loadBarris(path string) (map[int]*Barri, error) {
	idx, rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	c, err := col(idx, path, "codi_districte", "nom_districte", "codi_barri", "nom_barri")
	if err != nil {
		return nil, err
	}
	out := map[int]*Barri{}
	for _, r := range rows {
		b := &Barri{Code: atoi(r[c[2]]), Name: r[c[3]], DistrictCode: atoi(r[c[0]]), District: r[c[1]]}
		out[b.Code] = b
	}
	return out, nil
}

func loadSections(path string) (map[int]*Section, error) {
	idx, rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	c, err := col(idx, path, "codi_districte", "codi_barri", "codi_seccio_censal", "geometria_wgs84")
	if err != nil {
		return nil, err
	}
	out := map[int]*Section{}
	for _, r := range rows {
		sh, err := parseWKT(r[c[3]])
		if err != nil {
			return nil, fmt.Errorf("section %s/%s: %w", r[c[0]], r[c[2]], err)
		}
		key := atoi(r[c[0]])*1000 + atoi(r[c[2]])
		out[key] = &Section{Key: key, Barri: atoi(r[c[1]]), Shape: sh}
	}
	return out, nil
}

// loadPopulation adds padró residents (both sexes summed) to each section.
// Seccio_Censal in the padró file is already district*1000+section.
func loadPopulation(path string, secs map[int]*Section) (total float64, missing int, err error) {
	idx, rows, err := readCSV(path)
	if err != nil {
		return 0, 0, err
	}
	c, err := col(idx, path, "Seccio_Censal", "Valor")
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		v, perr := strconv.ParseFloat(strings.TrimSpace(r[c[1]]), 64)
		if perr != nil {
			continue // suppressed or empty cells
		}
		s, ok := secs[atoi(r[c[0]])]
		if !ok {
			missing++
			continue
		}
		s.Pop += v
		total += v
	}
	return total, missing, nil
}

// loadIncome sets the per-person disposable household income of each section.
func loadIncome(path string, secs map[int]*Section) (matched int, err error) {
	idx, rows, err := readCSV(path)
	if err != nil {
		return 0, err
	}
	c, err := col(idx, path, "Codi_Districte", "Seccio_Censal", "Import_Euros")
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		s, ok := secs[atoi(r[c[0]])*1000+atoi(r[c[1]])]
		v, perr := strconv.ParseFloat(strings.TrimSpace(r[c[2]]), 64)
		if ok && perr == nil {
			s.Income = v
			matched++
		}
	}
	return matched, nil
}

const sampleStepM = 20.0

// distributePopulation spreads each section's residents evenly over a global
// ~20 m lattice of points inside it and sums them per H3 cell (area-weighted
// dasymetric allocation). Sections too small to hold a lattice point fall back
// to their centroid.
func distributePopulation(secs map[int]*Section, res int) (map[h3.Cell]*Hex, error) {
	const latRef = 41.39
	dLat := sampleStepM / 111320.0
	dLon := sampleStepM / (111320.0 * math.Cos(latRef*math.Pi/180))
	hexes := map[h3.Cell]*Hex{}
	add := func(lat, lon, pop, income float64, barri int) error {
		cell, err := h3.LatLngToCell(h3.NewLatLng(lat, lon), res)
		if err != nil {
			return err
		}
		h := hexes[cell]
		if h == nil {
			h = &Hex{Cell: cell, barriPop: map[int]float64{}}
			hexes[cell] = h
		}
		h.Pop += pop
		h.samples++
		// tiny weight keeps barri assignment possible for unpopulated land
		h.barriPop[barri] += pop + 1e-6
		if income > 0 {
			h.incomePop += pop
			h.incomeSum += pop * income
		}
		return nil
	}
	for _, s := range secs {
		var pts []pt
		sh := s.Shape
		for i := math.Ceil(sh.MinLat / dLat); i*dLat <= sh.MaxLat; i++ {
			lat := i * dLat
			for j := math.Ceil(sh.MinLon / dLon); j*dLon <= sh.MaxLon; j++ {
				lon := j * dLon
				if sh.Contains(lon, lat) {
					pts = append(pts, pt{lon, lat})
				}
			}
		}
		if len(pts) == 0 {
			pts = []pt{sh.Centroid()}
		}
		share := s.Pop / float64(len(pts))
		for _, p := range pts {
			if err := add(p.Lat, p.Lon, share, s.Income, s.Barri); err != nil {
				return nil, err
			}
		}
	}
	for _, h := range hexes {
		best, bestPop := 0, -1.0
		for b, p := range h.barriPop {
			if p > bestPop || (p == bestPop && b < best) {
				best, bestPop = b, p
			}
		}
		h.Barri = best
	}
	return hexes, nil
}

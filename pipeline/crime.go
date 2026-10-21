package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Mossos "fets penals coneguts" types counted as street-relevant crime. Fraud,
// forgery, traffic and justice-administration offences are left out because
// they say little about the street a flat is on.
var crimeTypes = []string{
	"furt",
	"robatori amb violencia i o intimidacio",
	"robatori amb forca",
	"robatori amb forca interior vehicle",
	"robatori i furt d us de vehicle",
	"danys",
	"lesions",
	"amenaces",
	"agressions sexuals i abusos sexuals",
	"contra la salut publica",
	"homicidi",
	"ocupacio immobles",
	"altres delictes contra l ordre public",
}

func isCrimeType(t string) bool {
	n := norm(t)
	for _, c := range crimeTypes {
		if n == c || (c == "homicidi" && strings.HasPrefix(n, c)) {
			return true
		}
	}
	return false
}

type mossosRow struct {
	ABP   string `json:"rea_b_sica_policial_abp"`
	Type  string `json:"tipus_de_fet"`
	Year  string `json:"any"`
	Month string `json:"mes"`
	N     string `json:"n"`
}

type crimeResult struct {
	From, To     month
	ByDistrict   map[int]float64 // selected crimes per district over the window
	ByType       map[string]float64
	ByBarri      map[int]float64 // district crimes disaggregated to barris
	GubFrom      int
	GubTo        int
	GubByBarri   map[int]float64
	UnmatchedABP []string
}

func loadMossos(path string, months int, districts map[string]int) (*crimeResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []mossosRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var latest month
	for _, r := range rows {
		if m := mkMonth(atoi(r.Year), atoi(r.Month)); m > latest {
			latest = m
		}
	}
	res := &crimeResult{To: latest, From: latest - month(months-1), ByDistrict: map[int]float64{}, ByType: map[string]float64{}}
	unmatched := map[string]bool{}
	for _, r := range rows {
		m := mkMonth(atoi(r.Year), atoi(r.Month))
		if m < res.From || m > res.To || !isCrimeType(r.Type) {
			continue
		}
		d, ok := districts[norm(strings.TrimPrefix(r.ABP, "ABP "))]
		if !ok {
			unmatched[r.ABP] = true
			continue
		}
		n, _ := strconv.ParseFloat(r.N, 64)
		res.ByDistrict[d] += n
		res.ByType[r.Type] += n
	}
	for a := range unmatched {
		res.UnmatchedABP = append(res.UnmatchedABP, a)
	}
	sort.Strings(res.UnmatchedABP)
	return res, nil
}

// GUB incident codes used as the within-district distribution proxy:
// street public-order incidents attended by the city police.
var gubProxyCodes = map[string]string{
	"410": "Activitats molestes en espais públics",
	"411": "Venda ambulant",
	"510": "Baralles",
}

func loadGUB(paths []string) (map[int]float64, error) {
	out := map[int]float64{}
	for _, p := range paths {
		idx, rows, err := readCSV(p)
		if err != nil {
			return nil, err
		}
		c, err := col(idx, p, "Codi_Incident", "Codi_barri", "Numero_incidents_GUB")
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if _, ok := gubProxyCodes[strings.TrimSpace(r[c[0]])]; !ok || strings.TrimSpace(r[c[1]]) == "" {
				continue
			}
			n, _ := strconv.ParseFloat(strings.TrimSpace(r[c[2]]), 64)
			out[atoi(r[c[1]])] += n
		}
	}
	return out, nil
}

// disaggregate splits each district's crime count over its barris, with share
// alpha*GUB-share + (1-alpha)*population-share.
func (cr *crimeResult) disaggregate(barris map[int]*Barri, barriPop map[int]float64, alpha float64) {
	gubD, popD := map[int]float64{}, map[int]float64{}
	for code, b := range barris {
		gubD[b.DistrictCode] += cr.GubByBarri[code]
		popD[b.DistrictCode] += barriPop[code]
	}
	cr.ByBarri = map[int]float64{}
	for code, b := range barris {
		d := b.DistrictCode
		popShare, gubShare := 0.0, 0.0
		if popD[d] > 0 {
			popShare = barriPop[code] / popD[d]
		}
		a := alpha
		if gubD[d] > 0 {
			gubShare = cr.GubByBarri[code] / gubD[d]
		} else {
			a = 0
		}
		cr.ByBarri[code] = cr.ByDistrict[d] * (a*gubShare + (1-a)*popShare)
	}
}

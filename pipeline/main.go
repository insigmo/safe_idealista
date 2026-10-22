// Command bcngrid builds the Safe Idealista grid: per-H3-cell percentiles
// for dirt, smell, graffiti, incivism (IRIS complaints) and crime (Mossos,
// distributed within districts with Guàrdia Urbana incidents), normalised by
// residents.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

func logf(format string, args ...any) { log.Printf(format, args...) }

type RawCounts struct {
	Dirt     int `json:"dirt"`
	Smell    int `json:"smell"`
	Graffiti int `json:"graffiti"`
	Incivism int `json:"incivism"`
}

type CellOut struct {
	Dirt     int       `json:"dirt"`
	Smell    int       `json:"smell"`
	Graffiti int       `json:"graffiti"`
	Incivism int       `json:"incivism"`
	Crime    int       `json:"crime"`
	Score    int       `json:"score"`
	Pop      int       `json:"pop"`
	Barri    int       `json:"barri"`
	Income   int       `json:"inc,omitempty"`
	LowPop   bool      `json:"lp,omitempty"`
	Raw      RawCounts `json:"n_raw"`
}

type BarriOut struct {
	Name       string    `json:"name"`
	District   string    `json:"district"`
	Pop        int       `json:"pop"`
	Dirt       int       `json:"dirt"`
	Smell      int       `json:"smell"`
	Graffiti   int       `json:"graffiti"`
	Incivism   int       `json:"incivism"`
	Crime      int       `json:"crime"`
	Score      int       `json:"score"`
	Income     int       `json:"inc,omitempty"`
	CrimeEst   int       `json:"crime_est"`
	CrimePer1k float64   `json:"crime_per_1k_year"`
	Raw        RawCounts `json:"n_raw"`
}

type Range struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Meta struct {
	GeneratedAt  string             `json:"generated_at"`
	H3Resolution int                `json:"h3_resolution"`
	Categories   []string           `json:"categories"`
	Weights      map[string]float64 `json:"default_weights"`
	Ranges       map[string]Range   `json:"date_ranges"`
	Sources      []Source           `json:"sources"`
	Method       []string           `json:"method"`
	Caveats      []string           `json:"caveats"`
	Totals       map[string]int     `json:"totals"`
}

type Grid struct {
	Meta   Meta                `json:"meta"`
	Cells  map[string]CellOut  `json:"cells"`
	Barris map[string]BarriOut `json:"barris"`
}

func main() {
	cacheDir := flag.String("cache", ".cache", "download cache directory")
	outDir := flag.String("out", "out", "directory for grid.json and review artefacts")
	refresh := flag.Bool("refresh", false, "re-download cached source files")
	months := flag.Int("months", 24, "length of the complaint/crime window in months")
	res := flag.Int("res", 9, "H3 resolution")
	alpha := flag.Float64("crime-gub-alpha", 0.5, "weight of GUB incidents (vs population) when splitting district crime over barris")
	only := flag.String("only", "", "build only one layer: bcn (Barcelona grid), madrid (Madrid grid) or spain (Spain-wide shards)")
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("bcngrid: ")
	if *only != "" && *only != "bcn" && *only != "madrid" && *only != "spain" {
		log.Fatalf("-only must be bcn, madrid or spain, not %q", *only)
	}
	f, err := newFetcher(*cacheDir, *refresh)
	if err == nil {
		err = os.MkdirAll(*outDir, 0o755)
	}
	if err == nil && (*only == "" || *only == "bcn") {
		err = run(f, *outDir, *months, *res, *alpha)
	}
	if err == nil && (*only == "" || *only == "madrid") {
		err = runMadrid(f, *outDir, *months, *res)
	}
	if err == nil && (*only == "" || *only == "spain") {
		err = runSpain(f, *outDir)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func run(f *fetcher, outDir string, months, res int, alpha float64) error {
	var sources []Source
	ranges := map[string]Range{}

	// --- geometry, population, income ---
	units, err := f.ckanPackage("20170706-districtes-barris")
	if err != nil {
		return err
	}
	var secPath, barriPath string
	for _, r := range units.Resources {
		var p string
		var src Source
		switch r.Name {
		case "BarcelonaCiutat_SeccionsCensals.csv":
			p, src, err = f.ckanFile(units, r, "seccions_censals.csv")
			secPath = p
		case "BarcelonaCiutat_Barris.csv":
			p, src, err = f.ckanFile(units, r, "barris.csv")
			barriPath = p
		default:
			continue
		}
		if err != nil {
			return err
		}
		sources = append(sources, src)
	}
	if secPath == "" || barriPath == "" {
		return fmt.Errorf("administrative units: section or barri CSV not found")
	}
	barris, err := loadBarris(barriPath)
	if err != nil {
		return err
	}
	secs, err := loadSections(secPath)
	if err != nil {
		return err
	}

	popPkg, err := f.ckanPackage("pad_mdbas_sexe")
	if err != nil {
		return err
	}
	popRes := yearlyCSV(popPkg, regexp.MustCompile(`^(\d{4})_pad_mdbas_sexe\.csv$`))
	if len(popRes) == 0 {
		return fmt.Errorf("pad_mdbas_sexe: no yearly CSV")
	}
	popPath, src, err := f.ckanFile(popPkg, popRes[0].Res, fmt.Sprintf("pad_mdbas_sexe_%d.csv", popRes[0].Year))
	if err != nil {
		return err
	}
	sources = append(sources, src)
	ranges["population"] = Range{fmt.Sprintf("%d-01-01", popRes[0].Year), fmt.Sprintf("%d-01-01", popRes[0].Year)}
	totalPop, missingSec, err := loadPopulation(popPath, secs)
	if err != nil {
		return err
	}
	logf("population %d: %.0f residents in %d sections (%d padró rows without geometry)", popRes[0].Year, totalPop, len(secs), missingSec)

	incPkg, err := f.ckanPackage("renda-disponible-llars-bcn")
	if err != nil {
		return err
	}
	incRes := yearlyCSV(incPkg, regexp.MustCompile(`^(\d{4})_renda_disponible_llars_per_persona\.csv$`))
	if len(incRes) > 0 {
		incPath, src, err := f.ckanFile(incPkg, incRes[0].Res, fmt.Sprintf("renda_%d.csv", incRes[0].Year))
		if err != nil {
			return err
		}
		sources = append(sources, src)
		ranges["income"] = Range{fmt.Sprint(incRes[0].Year), fmt.Sprint(incRes[0].Year)}
		n, err := loadIncome(incPath, secs)
		if err != nil {
			return err
		}
		logf("income %d: matched %d of %d sections", incRes[0].Year, n, len(secs))
	}

	hexes, err := distributePopulation(secs, res)
	if err != nil {
		return err
	}
	logf("H3 r%d: %d cells cover the city", res, len(hexes))
	barriPop := map[int]float64{}
	for _, s := range secs {
		barriPop[s.Barri] += s.Pop
	}

	// --- IRIS complaints ---
	irisPkg, err := f.ckanPackage("iris")
	if err != nil {
		return err
	}
	irisRes := yearlyCSV(irisPkg, regexp.MustCompile(`^(\d{4})_IRIS_Peticions_Ciutadanes_OpenData\.csv$`))
	need := (months+11)/12 + 1 // years that can overlap a window ending in the newest file
	if len(irisRes) < need {
		need = len(irisRes)
	}
	var irisPaths []string
	for _, yr := range irisRes[:need] {
		p, src, err := f.ckanFile(irisPkg, yr.Res, fmt.Sprintf("iris_%d.csv", yr.Year))
		if err != nil {
			return err
		}
		irisPaths = append(irisPaths, p)
		sources = append(sources, src)
	}
	recs, ist, err := loadIRIS(irisPaths)
	if err != nil {
		return err
	}
	to := latestFullMonth(ist.ByMonth, 1000)
	from := to - month(months-1)
	if from < mkMonth(irisRes[need-1].Year, 1) {
		return fmt.Errorf("IRIS window starts %s, before the oldest downloaded file", from)
	}
	ranges["iris"] = Range{from.String(), to.String()}
	logf("IRIS: %d rows; %d complaint records with coordinates; window %s..%s", ist.Rows, len(recs), from, to)

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
			return err
		}
		h := hexes[cell]
		if h == nil {
			outside++ // geocoded to sea, port or beyond the municipal boundary
			continue
		}
		h.Counts[mr.Cat]++
		totals[irisCatNames[mr.Cat]]++
	}
	logf("IRIS: %d mapped records fell outside populated-land cells and were dropped", outside)
	totals["iris_dropped_outside"] = outside

	// --- crime ---
	districts := map[string]int{}
	for _, b := range barris {
		districts[norm(b.District)] = b.DistrictCode
	}
	q := url.Values{}
	q.Set("$select", "rea_b_sica_policial_abp,tipus_de_fet,any,mes,sum(coneguts) as n")
	q.Set("$where", "regi_policial_rp='RP Metropolitana Barcelona'")
	q.Set("$group", "rea_b_sica_policial_abp,tipus_de_fet,any,mes")
	q.Set("$limit", "200000")
	mossosPath, src, err := f.socrata("qnyt-emjc", q, "mossos_fets_penals_bcn.json")
	if err != nil {
		return err
	}
	src.Title = "Fets penals coneguts, fets coneguts resolts i detencions (Mossos d'Esquadra)"
	src.Resource = "SoQL aggregate by ABP, type, month — RP Metropolitana Barcelona"
	sources = append(sources, src)
	cr, err := loadMossos(mossosPath, months, districts)
	if err != nil {
		return err
	}
	ranges["crime"] = Range{cr.From.String(), cr.To.String()}
	logf("Mossos: window %s..%s; ABPs without a district (ignored): %v", cr.From, cr.To, cr.UnmatchedABP)

	gubPkg, err := f.ckanPackage("incidents-gestionats-gub")
	if err != nil {
		return err
	}
	gubRes := yearlyCSV(gubPkg, regexp.MustCompile(`(?i)^(\d{4})_incidents_gestionats_gub\.csv$`))
	if len(gubRes) < 2 {
		return fmt.Errorf("incidents-gestionats-gub: need two yearly CSVs")
	}
	var gubPaths []string
	for _, yr := range gubRes[:2] {
		p, src, err := f.ckanFile(gubPkg, yr.Res, fmt.Sprintf("gub_%d.csv", yr.Year))
		if err != nil {
			return err
		}
		gubPaths = append(gubPaths, p)
		sources = append(sources, src)
	}
	cr.GubFrom, cr.GubTo = gubRes[1].Year, gubRes[0].Year
	ranges["gub_incidents"] = Range{fmt.Sprintf("%d-01", cr.GubFrom), fmt.Sprintf("%d-12", cr.GubTo)}
	if cr.GubByBarri, err = loadGUB(gubPaths); err != nil {
		return err
	}
	cr.disaggregate(barris, barriPop, alpha)
	crimeRate := barriCrimeRates(cr.ByBarri, barriPop)

	// --- rates, percentiles, scores ---
	pcts := map[string]map[h3.Cell]float64{}
	for cat := 0; cat < nIris; cat++ {
		pcts[irisCatNames[cat]] = percentiles(hexes, hexRates(hexes, cat))
	}
	crimeVals := map[h3.Cell]float64{}
	for c, h := range hexes {
		crimeVals[c] = crimeRate[h.Barri]
	}
	pcts["crime"] = percentiles(hexes, crimeVals)

	grid := Grid{Cells: map[string]CellOut{}, Barris: map[string]BarriOut{}}
	type acc struct {
		pop, inc, incPop float64
		pct              map[string]float64
		raw              [nIris]float64
	}
	bacc := map[int]*acc{}
	for c, h := range hexes {
		p := map[string]float64{}
		for _, k := range catOrder {
			p[k] = pcts[k][c]
		}
		grid.Cells[c.String()] = CellOut{
			Dirt: round(p["dirt"]), Smell: round(p["smell"]), Graffiti: round(p["graffiti"]),
			Incivism: round(p["incivism"]), Crime: round(p["crime"]),
			Score: round(score(p, defaultWeights)), Pop: round(h.Pop), Barri: h.Barri,
			Income: round(h.Income()), LowPop: h.Pop < residentialMinPop,
			Raw: RawCounts{round(h.Counts[catDirt]), round(h.Counts[catSmell]), round(h.Counts[catGraffiti]), round(h.Counts[catIncivism])},
		}
		// A hex can straddle barris: credit each one by its residents in the
		// hex, so small barris without a majority hex still get values.
		var wsum float64
		for _, w := range h.barriPop {
			wsum += w
		}
		for b, w := range h.barriPop {
			a := bacc[b]
			if a == nil {
				a = &acc{pct: map[string]float64{}}
				bacc[b] = a
			}
			a.pop += w
			if h.Income() > 0 {
				a.inc += h.Income() * w
				a.incPop += w
			}
			for _, k := range catOrder {
				a.pct[k] += p[k] * w
			}
			for i := range a.raw {
				a.raw[i] += h.Counts[i] * w / wsum
			}
		}
	}
	for code, b := range barris {
		a := bacc[code]
		if a == nil || a.pop == 0 {
			continue
		}
		p := map[string]float64{}
		for _, k := range catOrder {
			p[k] = a.pct[k] / a.pop
		}
		bo := BarriOut{
			Name: b.Name, District: b.District, Pop: round(barriPop[code]),
			Dirt: round(p["dirt"]), Smell: round(p["smell"]), Graffiti: round(p["graffiti"]),
			Incivism: round(p["incivism"]), Crime: round(p["crime"]), Score: round(score(p, defaultWeights)),
			CrimeEst: round(cr.ByBarri[code]),
			Raw:      RawCounts{round(a.raw[0]), round(a.raw[1]), round(a.raw[2]), round(a.raw[3])},
		}
		if barriPop[code] > 0 {
			yearsSpan := float64(months) / 12
			bo.CrimePer1k = float64(round(10*cr.ByBarri[code]/barriPop[code]*1000/yearsSpan)) / 10
		}
		if a.incPop > 0 {
			bo.Income = round(a.inc / a.incPop)
		}
		grid.Barris[fmt.Sprint(code)] = bo
	}
	var crimeTotal float64
	for _, v := range cr.ByDistrict {
		crimeTotal += v
	}
	totals["crime"] = round(crimeTotal)
	totals["cells"] = len(grid.Cells)
	totals["population"] = round(totalPop)

	grid.Meta = Meta{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		H3Resolution: res,
		Categories:   catOrder,
		Weights:      defaultWeights,
		Ranges:       ranges,
		Sources:      sources,
		Totals:       totals,
		Method:       methodNotes(months, alpha),
		Caveats:      caveats,
	}

	if err := writeJSON(filepath.Join(outDir, "grid.json"), grid); err != nil {
		return err
	}
	if err := writeMappingReport(filepath.Join(outDir, "mapping.md"), mapping, from, to); err != nil {
		return err
	}
	if err := writeRankings(filepath.Join(outDir, "rankings.md"), grid, cr); err != nil {
		return err
	}
	return writeReviewMap(filepath.Join(outDir, "review_map.html"), grid, hexes)
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	logf("wrote %s (%d KB)", path, len(b)/1024)
	return os.WriteFile(path, b, 0o644)
}

func methodNotes(months int, alpha float64) []string {
	return []string{
		fmt.Sprintf("IRIS incidents and complaints (not queries/suggestions) with coordinates, last %d months available, mapped to categories by AREA/ELEMENT/DETALL rules.", months),
		"Residents per H3 cell: padró population per census section spread evenly over the section's area (20 m sample lattice).",
		fmt.Sprintf("Per-cell rate = (count + ½·neighbour counts + %d·city rate) / (residents + ½·neighbour residents + %d): ring smoothing plus shrinkage toward the city rate.", hexPriorPop, hexPriorPop),
		fmt.Sprintf("Crime: Mossos 'fets penals coneguts' (street-relevant types) per district (ABP), last %d months; split over barris by %.0f%% Guàrdia Urbana public-order incidents share + %.0f%% population share; rate per resident shrunk toward the city rate (%d residents).", months, alpha*100, (1-alpha)*100, barriPriorPop),
		fmt.Sprintf("Values are city-wide percentiles (0 = cleanest/safest, 100 = worst) against cells with ≥%d residents. Score = 100 − weighted mean of percentiles.", residentialMinPop),
	}
}

var caveats = []string{
	"Complaints measure reporting, not reality: dense, central and tourist areas report more, and some neighbourhoods rarely use IRIS.",
	"Normalising by residents inflates areas with few residents and many visitors (old town, commercial streets).",
	"Crime is official only at district level (10 Mossos ABPs); the within-district split is an estimate from city-police incident counts.",
	"Data is not live: see the date ranges. IRIS and police data lag by several months.",
	"Cells with fewer than 100 residents (parks, port, industrial land) are flagged low-population; their values are mostly the city average.",
}

// sortedKeys is a helper for deterministic report output.
func sortedKeys[M ~map[string]V, V any](m M) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

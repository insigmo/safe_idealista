package main

// Spain-wide layer, used by the extension outside Barcelona's own grid:
// street crime per municipality (Interior Ministry; only municipalities over
// 20,000 residents are published) and household income per census section
// (INE, Atlas de Distribución de Renta de los Hogares). Census sections are
// indexed as compacted H3 cells and written as shards keyed by their res-3
// ancestor, so the extension only loads the region a listing is in.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

const (
	ineTablesURL   = "https://servicios.ine.es/wstempus/js/ES/TABLAS_OPERACION/353" // 353 = ADRH
	ineTableCSV    = "https://www.ine.es/jaxiT3/files/t/es/csv_bdsc/%d.csv"
	ineSectionsZip = "https://www.ine.es/prodyser/cartografia/seccionado_%d.zip"
	mirPortal      = "https://estadisticasdecriminalidad.ses.mir.es"

	// Finest H3 resolution of the section index. Sections too small to hold
	// a res-9 cell centre get one cell one resolution finer.
	esCellRes  = 9
	esShardRes = 3 // ~12,000 km² per shard
	// ADRH indicators used, as named in the INE tables.
	adrhIncome     = "Renta neta media por persona"
	adrhPopulation = "Población"
	// Spain has ~36,500 census sections; a year with this many is the
	// whole country.
	adrhSpainSections = 35000
	// INE publishes the whole country, the Canaries included, in UTM zone 30.
	utmZone30CM = -3.0
)

var (
	// Balance de Criminalidad types counted as street crime: 1-10 are
	// homicide, attempted homicide, assault, kidnapping, sexual offences,
	// robbery with violence, burglary, theft, vehicle theft and drug
	// trafficking. "11. Resto" and cybercrime are left out.
	mirTypeRe   = regexp.MustCompile(`^(\d+)\. `)
	mirMuniRe   = regexp.MustCompile(`^(\d{5}) (.+)$`)
	mirYearRe   = regexp.MustCompile(`path=/DatosBalanceAnt/(\d{4})4/`)
	mirExportRe = regexp.MustCompile(`dlgExport\.htm\?path=/DatosBalanceAnt//&(?:amp;)?file=(\d+)\.px`)
	mirTitleRe  = regexp.MustCompile(`Balance trimestral de Criminalidad[^<]*`)
	adrhCodeRe  = regexp.MustCompile(`^(\d+) (.+)$`)
	// INE moves a leading article to the end: "Palmas de Gran Canaria, Las".
	trailingArticleRe = regexp.MustCompile(`(?i)^(.+), (el|la|los|las|l'|lo|els|les|es|sa|ses|o|a|os|as)$`)
)

// displayName restores the article INE moved to the end of a municipality
// name, in each part of a bilingual name ("Vila Joiosa, la/Villajoyosa").
func displayName(s string) string {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		m := trailingArticleRe.FindStringSubmatch(strings.TrimSpace(p))
		if m == nil {
			continue
		}
		art := strings.ToUpper(m[2][:1]) + m[2][1:]
		if strings.HasSuffix(art, "'") {
			parts[i] = art + m[1]
		} else {
			parts[i] = art + " " + m[1]
		}
	}
	return strings.Join(parts, "/")
}

type esSection struct {
	Code   string
	Muni   string
	Shape  *Shape
	Income float64 // € per person, 0 when unknown
	Pop    float64
}

type esMuni struct {
	Name   string
	Pop    float64
	Income float64
	Crime  float64 // street crimes in the year; -1 when not published
}

// esMuniOut is a municipality as the extension sees it.
type esMuniOut struct {
	Name   string   `json:"n"`
	Pop    int      `json:"pop"`
	Income int      `json:"inc,omitempty"`
	Crime  *int     `json:"crime,omitempty"` // percentile, 0 = safest
	Per1k  *float64 `json:"c1k,omitempty"`   // street crimes per 1,000 residents per year
	Count  *int     `json:"cn,omitempty"`
}

// esShard holds sections as [municipality, € per person, income percentile
// (0 = richest), residents]; income fields are null when INE suppressed them.
type esShard struct {
	Munis    map[string]esMuniOut `json:"m"`
	Sections [][4]any             `json:"s"`
	Cells    map[string]int       `json:"c"`
}

type esIndex struct {
	Meta     Meta     `json:"meta"`
	CellRes  int      `json:"cell_res"`
	ShardRes int      `json:"shard_res"`
	Shards   []string `json:"shards"`
}

func runSpain(f *fetcher, outDir string) error {
	var sources []Source
	ranges := map[string]Range{}

	adrh, err := loadADRH(f)
	if err != nil {
		return err
	}
	sources = append(sources, adrh.Sources...)
	ranges["income"] = Range{fmt.Sprint(adrh.Year), fmt.Sprint(adrh.Year)}
	logf("ADRH %d: income for %d sections, population for %d sections, %d municipalities", adrh.Year, len(adrh.SecIncome), len(adrh.SecPop), len(adrh.Munis))

	crime, crimeYear, src, err := loadMIR(f)
	if err != nil {
		return err
	}
	sources = append(sources, src)
	ranges["crime"] = Range{fmt.Sprintf("%d-01", crimeYear), fmt.Sprintf("%d-12", crimeYear)}
	var matched int
	for code, m := range adrh.Munis {
		m.Crime = -1
		if n, ok := crime[code]; ok {
			m.Crime = n
			matched++
		}
	}
	logf("crime %d: %d municipalities published, %d matched to INE municipalities", crimeYear, len(crime), matched)

	secs, secYear, src, err := loadINESections(f, adrh)
	if err != nil {
		return err
	}
	sources = append(sources, src)
	ranges["sections"] = Range{fmt.Sprintf("%d-01-01", secYear), fmt.Sprintf("%d-01-01", secYear)}

	// --- percentiles: population-weighted, so 30 means 30% of residents
	// live somewhere with a lower rate / higher income ---
	var crimeVals, crimeW []float64
	for _, m := range adrh.Munis {
		if m.Crime >= 0 && m.Pop > 0 {
			crimeVals = append(crimeVals, m.Crime/m.Pop)
			crimeW = append(crimeW, m.Pop)
		}
	}
	crimePct := weightedPercentiles(crimeVals, crimeW)
	var incVals, incW []float64
	for _, s := range secs {
		if s.Income > 0 && s.Pop > 0 {
			incVals = append(incVals, s.Income)
			incW = append(incW, s.Pop)
		}
	}
	incPct := weightedPercentiles(incVals, incW)

	munisOut := map[string]esMuniOut{}
	var popWithCrime, popTotal float64
	for code, m := range adrh.Munis {
		o := esMuniOut{Name: m.Name, Pop: round(m.Pop), Income: round(m.Income)}
		popTotal += m.Pop
		if m.Crime >= 0 && m.Pop > 0 {
			p := round(crimePct(m.Crime / m.Pop))
			per1k := float64(round(10*m.Crime/m.Pop*1000)) / 10
			n := round(m.Crime)
			o.Crime, o.Per1k, o.Count = &p, &per1k, &n
			popWithCrime += m.Pop
		}
		munisOut[code] = o
	}

	// --- H3 index of the sections ---
	owner, small, dropped, err := indexSections(secs)
	if err != nil {
		return err
	}
	logf("H3: %d compacted cells for %d sections (%d sections only at res %d, %d too small to place)", len(owner), len(secs), small, esCellRes+1, dropped)

	shards := map[string]*esShard{}
	local := map[string]map[int]int{} // shard -> section index -> position in shard
	for cell, si := range owner {
		key, err := cell.Parent(esShardRes)
		if err != nil {
			return err
		}
		k := key.String()
		sh := shards[k]
		if sh == nil {
			sh = &esShard{Munis: map[string]esMuniOut{}, Cells: map[string]int{}}
			shards[k] = sh
			local[k] = map[int]int{}
		}
		pos, ok := local[k][si]
		if !ok {
			s := secs[si]
			var inc, bad any
			if s.Income > 0 {
				inc, bad = round(s.Income), round(100-incPct(s.Income))
			}
			pos = len(sh.Sections)
			sh.Sections = append(sh.Sections, [4]any{s.Muni, inc, bad, round(s.Pop)})
			local[k][si] = pos
			if mo, ok := munisOut[s.Muni]; ok {
				sh.Munis[s.Muni] = mo
			}
		}
		sh.Cells[cell.String()] = pos
	}

	dir := filepath.Join(outDir, "spain")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keys := sortedKeys(shards)
	var bytesTotal int
	for _, k := range keys {
		b, err := json.Marshal(shards[k])
		if err != nil {
			return err
		}
		bytesTotal += len(b)
		if err := os.WriteFile(filepath.Join(dir, k+".json"), b, 0o644); err != nil {
			return err
		}
	}
	logf("wrote %d shards to %s (%d KB)", len(keys), dir, bytesTotal/1024)

	idx := esIndex{CellRes: esCellRes, ShardRes: esShardRes, Shards: keys, Meta: Meta{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		H3Resolution: esCellRes,
		Categories:   []string{"crime", "income"},
		Weights:      map[string]float64{"crime": 0.35, "income": 0.15},
		Ranges:       ranges,
		Sources:      sources,
		Method:       spainMethod(crimeYear, adrh.Year),
		Caveats:      spainCaveats,
		Totals: map[string]int{
			"sections": len(secs), "municipalities": len(adrh.Munis), "municipalities_with_crime": matched,
			"population": round(popTotal), "population_with_crime": round(popWithCrime), "cells": len(owner),
		},
	}}
	if err := writeJSON(filepath.Join(dir, "index.json"), idx); err != nil {
		return err
	}
	return writeSpainSummary(filepath.Join(dir, "summary.md"), idx, munisOut, bytesTotal)
}

type adrhData struct {
	Year      int
	SecIncome map[string]float64
	SecPop    map[string]float64
	Munis     map[string]*esMuni
	Sources   []Source
}

type ineTable struct {
	ID       int    `json:"Id"`
	Name     string `json:"Nombre"`
	Modified int64  `json:"Ultima_Modificacion"`
}

// loadADRH downloads the per-province ADRH tables (income, demographics) and
// keeps the newest year that covers nearly every census section.
func loadADRH(f *fetcher) (*adrhData, error) {
	raw, err := f.fresh(ineTablesURL)
	if err != nil {
		return nil, err
	}
	var tables []ineTable
	if err := json.Unmarshal(raw, &tables); err != nil {
		return nil, fmt.Errorf("INE table list: %w", err)
	}
	// year -> code -> value, for sections ("s") and municipalities ("m")
	type series map[int]map[string]float64
	inc := map[string]series{"s": {}, "m": {}}
	pop := map[string]series{"s": {}, "m": {}}
	names := map[string]string{}
	d := &adrhData{}
	var nInc, nPop int
	// The list holds one table per province plus one for the whole country
	// (~400 MB, second in the list today). Stop fetching a kind once the
	// tables read so far cover the country.
	complete := func(s map[string]series) bool {
		for _, m := range s["s"] {
			if len(m) >= adrhSpainSections {
				return true
			}
		}
		return false
	}
	for _, t := range tables {
		var want string
		var dst map[string]series
		switch {
		case strings.HasPrefix(t.Name, "Indicadores de renta media"):
			if complete(inc) {
				continue
			}
			want, dst = adrhIncome, inc
			nInc++
		case strings.HasPrefix(t.Name, "Indicadores demogr"):
			if complete(pop) {
				continue
			}
			want, dst = adrhPopulation, pop
			nPop++
		default:
			continue
		}
		u := fmt.Sprintf(ineTableCSV, t.ID)
		p, err := f.get(u, fmt.Sprintf("ine_adrh_%d_%d.csv", t.ID, t.Modified))
		if err != nil {
			return nil, err
		}
		if err := readADRH(p, want, func(level, code, name string, year int, v float64) {
			m := dst[level][year]
			if m == nil {
				m = map[string]float64{}
				dst[level][year] = m
			}
			m[code] = v
			if level == "m" {
				names[code] = displayName(name)
			}
		}); err != nil {
			return nil, err
		}
	}
	if nInc == 0 || nPop == 0 {
		return nil, fmt.Errorf("INE ADRH: income or demographic tables not found (%d, %d)", nInc, nPop)
	}
	d.Sources = append(d.Sources,
		Source{Portal: "INE", Dataset: "ADRH (operation 353)", Title: "Atlas de Distribución de Renta de los Hogares — Indicadores de renta media y mediana", Resource: fmt.Sprintf("%d table(s), '%s'", nInc, adrhIncome), URL: ineTablesURL},
		Source{Portal: "INE", Dataset: "ADRH (operation 353)", Title: "Atlas de Distribución de Renta de los Hogares — Indicadores demográficos", Resource: fmt.Sprintf("%d table(s), '%s'", nPop, adrhPopulation), URL: ineTablesURL},
	)
	best := 0
	for _, m := range inc["s"] {
		if len(m) > best {
			best = len(m)
		}
	}
	for y, m := range inc["s"] {
		if float64(len(m)) >= 0.9*float64(best) && y > d.Year && len(pop["s"][y]) > 0 {
			d.Year = y
		}
	}
	if d.Year == 0 {
		return nil, fmt.Errorf("INE ADRH: no year with section income and population")
	}
	d.SecIncome, d.SecPop = inc["s"][d.Year], pop["s"][d.Year]
	d.Munis = map[string]*esMuni{}
	for code, name := range names {
		d.Munis[code] = &esMuni{Name: name, Pop: pop["m"][d.Year][code], Income: inc["m"][d.Year][code]}
	}
	return d, nil
}

// readADRH streams one ADRH CSV (Municipios;Distritos;Secciones;indicator;
// Periodo;Total) and reports the rows of one indicator at municipality or
// section level. Values use Spanish number format ("14.781,0").
func readADRH(path, indicator string, emit func(level, code, name string, year int, v float64)) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(header) < 6 || !strings.HasSuffix(header[0], "Municipios") || header[4] != "Periodo" {
		return fmt.Errorf("%s: unexpected header %q", path, header)
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(rec) < 6 || rec[3] != indicator {
			continue
		}
		v, ok := spanishNumber(rec[5])
		if !ok {
			continue
		}
		level, field := "m", rec[0]
		switch {
		case rec[2] != "":
			level, field = "s", rec[2]
		case rec[1] != "":
			continue // district
		}
		m := adrhCodeRe.FindStringSubmatch(field)
		if m == nil {
			continue
		}
		emit(level, m[1], m[2], atoi(rec[4]), v)
	}
}

func spanishNumber(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ".", "")
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	return v, err == nil
}

// loadMIR finds the newest full-year Balance de Criminalidad (4th quarter)
// and returns street crimes per municipality (INE code) for that year.
func loadMIR(f *fetcher) (map[string]float64, int, Source, error) {
	var src Source
	page, err := f.fresh(mirPortal + "/publico/portalestadistico/balances.html")
	if err != nil {
		return nil, 0, src, err
	}
	year := 0
	for _, m := range mirYearRe.FindAllSubmatch(page, -1) {
		year = max(year, atoi(string(m[1])))
	}
	if year == 0 {
		return nil, 0, src, fmt.Errorf("Balance de Criminalidad: no 4th-quarter release listed")
	}
	idxURL := fmt.Sprintf("%s/sec/dynPx/inebase/index.htm?type=pcaxis&path=/DatosBalanceAnt/%d4/&file=pcaxis", mirPortal, year)
	idx, err := f.fresh(idxURL)
	if err != nil {
		return nil, 0, src, err
	}
	// Each table's export link precedes its title; take the link before the
	// municipal table's title.
	file, title := "", ""
	exports := mirExportRe.FindAllSubmatchIndex(idx, -1)
	for _, t := range mirTitleRe.FindAllIndex(idx, -1) {
		text := string(idx[t[0]:t[1]])
		if !strings.Contains(text, "Municipios") {
			continue
		}
		for _, e := range exports {
			if e[0] < t[0] {
				file = string(idx[e[2]:e[3]])
			}
		}
		title = strings.TrimSpace(text)
		break
	}
	if file == "" {
		return nil, 0, src, fmt.Errorf("Balance de Criminalidad %d: municipal table not found", year)
	}
	u := fmt.Sprintf("%s/sec/jaxiPx/files/_px/es/csv_bdsc/DatosBalanceAnt/l0/%s.csv_bdsc?nocab=1", mirPortal, file)
	p, err := f.get(u, fmt.Sprintf("mir_balance_%d4_%s.csv", year, file))
	if err != nil {
		return nil, 0, src, err
	}
	src = Source{Portal: "Ministerio del Interior — Portal Estadístico de Criminalidad", Dataset: fmt.Sprintf("Balance de Criminalidad %d T4 (table %s)", year, file), Title: title, Resource: "types 1–10 (street crime), enero-diciembre " + fmt.Sprint(year), URL: u}

	fh, err := os.Open(p)
	if err != nil {
		return nil, 0, src, err
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	period := fmt.Sprintf("enero-diciembre %d", year)
	out := map[string]float64{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, src, fmt.Errorf("%s: %w", p, err)
		}
		if len(rec) < 4 || rec[2] != period {
			continue
		}
		m := mirMuniRe.FindStringSubmatch(rec[0])
		t := mirTypeRe.FindStringSubmatch(rec[1])
		if m == nil || t == nil {
			continue
		}
		if n := atoi(t[1]); n < 1 || n > 10 {
			continue
		}
		if v, ok := spanishNumber(rec[3]); ok {
			out[m[1]] += v
		}
	}
	if len(out) == 0 {
		return nil, 0, src, fmt.Errorf("%s: no municipal rows for %s", p, period)
	}
	return out, year, src, nil
}

// loadINESections reads INE's census-section boundaries for the year that
// matches the ADRH section codes (ADRH year t uses the sections of 1 January
// t+1), falling back to neighbouring years.
func loadINESections(f *fetcher, adrh *adrhData) ([]esSection, int, Source, error) {
	var lastErr error
	for _, y := range []int{adrh.Year + 1, adrh.Year + 2, adrh.Year} {
		u := fmt.Sprintf(ineSectionsZip, y)
		p, err := f.get(u, fmt.Sprintf("ine_seccionado_%d.zip", y))
		if err != nil {
			lastErr = err
			continue
		}
		recs, err := readZippedShapefile(p, func(x, y float64) pt { return utmToLonLat(x, y, utmZone30CM) })
		if err != nil {
			return nil, 0, Source{}, err
		}
		var secs []esSection
		var withIncome int
		for _, r := range recs {
			code := r.Attrs["CUSEC"]
			if len(code) != 10 {
				continue
			}
			s := esSection{Code: code, Muni: code[:5], Shape: r.Shape, Income: adrh.SecIncome[code], Pop: adrh.SecPop[code]}
			if s.Income > 0 {
				withIncome++
			}
			secs = append(secs, s)
		}
		share := float64(withIncome) / float64(len(adrh.SecIncome))
		logf("INE sections %d: %d polygons, %d of %d ADRH sections matched (%.1f%%)", y, len(secs), withIncome, len(adrh.SecIncome), 100*share)
		if share < 0.97 {
			lastErr = fmt.Errorf("INE sections %d: only %.1f%% of ADRH sections matched", y, 100*share)
			continue
		}
		src := Source{Portal: "INE", Dataset: "Cartografía digitalizada de secciones censales", Title: fmt.Sprintf("Seccionado %d (ETRS89, UTM 30)", y), Resource: filepath.Base(u), URL: u}
		return secs, y, src, nil
	}
	return nil, 0, Source{}, lastErr
}

const (
	// Coarsest cell the index uses (~250 km²).
	esCoarsestRes = 5
	// A cell coarser than res 9 is stored whole when every section under it
	// is at least this many times the cell's area; it then goes to the
	// section holding most of it. Rural sections are large and their borders
	// long, so this keeps the index small without blurring towns.
	esCoarseFactor = 4
)

// indexSections assigns every res-9 cell to the section containing its
// centre, then stores the result hierarchically: a coarser cell when only
// large sections lie under it (see esCoarseFactor), res-9 cells elsewhere.
// Sections too small for any res-9 centre get the res-10 cells inside them
// (or the one under their centroid), which the lookup checks first.
func indexSections(secs []esSection) (owner map[h3.Cell]int, small, dropped int, err error) {
	raster := map[h3.Cell]int32{}
	n9 := make([]int, len(secs)) // res-9 cells per section, i.e. its size
	owner = map[h3.Cell]int{}
	for i, s := range secs {
		cells, err := shapeCells(s.Shape, esCellRes)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("section %s: %w", s.Code, err)
		}
		for _, c := range cells {
			raster[c] = int32(i)
		}
		n9[i] = len(cells)
		if len(cells) > 0 {
			continue
		}
		small++
		if cells, err = shapeCells(s.Shape, esCellRes+1); err != nil {
			return nil, 0, 0, fmt.Errorf("section %s: %w", s.Code, err)
		}
		if len(cells) == 0 {
			c := s.Shape.Centroid()
			cell, err := h3.LatLngToCell(h3.NewLatLng(c.Lat, c.Lon), esCellRes+1)
			if err != nil {
				return nil, 0, 0, err
			}
			if _, taken := owner[cell]; taken {
				dropped++
				continue
			}
			cells = []h3.Cell{cell}
		}
		for _, c := range cells {
			owner[c] = i
		}
	}

	roots := map[h3.Cell]bool{}
	for c := range raster {
		p, err := c.Parent(esCoarsestRes)
		if err != nil {
			return nil, 0, 0, err
		}
		roots[p] = true
	}
	var visit func(c h3.Cell) error
	visit = func(c h3.Cell) error {
		r := c.Resolution()
		if r == esCellRes {
			if s, ok := raster[c]; ok {
				owner[c] = int(s)
			}
			return nil
		}
		leaves, err := c.Children(esCellRes)
		if err != nil {
			return err
		}
		need := esCoarseFactor * int(math.Pow(7, float64(esCellRes-r)))
		counts := map[int32]int{}
		coarse := true
		for _, l := range leaves {
			if s, ok := raster[l]; ok {
				counts[s]++
				coarse = coarse && n9[s] >= need
			}
		}
		if len(counts) == 0 {
			return nil // sea or outside Spain
		}
		if coarse {
			best, bestN := int32(-1), -1
			for s, n := range counts {
				if n > bestN || (n == bestN && s < best) {
					best, bestN = s, n
				}
			}
			owner[c] = int(best)
			return nil
		}
		kids, err := c.ImmediateChildren()
		if err != nil {
			return err
		}
		for _, k := range kids {
			if err := visit(k); err != nil {
				return err
			}
		}
		return nil
	}
	for c := range roots {
		if err := visit(c); err != nil {
			return nil, 0, 0, err
		}
	}
	return owner, small, dropped, nil
}

// shapeCells polyfills a shape (outer rings and holes, shapefile winding:
// outer rings clockwise) with res-cells whose centres are inside it.
func shapeCells(sh *Shape, res int) ([]h3.Cell, error) {
	var outers, holes [][]pt
	for _, r := range sh.Rings {
		if ringArea(r) < 0 {
			outers = append(outers, r)
		} else {
			holes = append(holes, r)
		}
	}
	if len(outers) == 0 { // non-standard winding: treat every ring as an outer ring
		outers, holes = holes, nil
	}
	polys := make([]h3.GeoPolygon, len(outers))
	for i, o := range outers {
		polys[i].GeoLoop = geoLoop(o)
	}
	for _, h := range holes {
		for i, o := range outers {
			if ringContains(o, h[0]) {
				polys[i].Holes = append(polys[i].Holes, geoLoop(h))
				break
			}
		}
	}
	seen := map[h3.Cell]bool{}
	var out []h3.Cell
	for _, p := range polys {
		cells, err := h3.PolygonToCellsExperimental(p, res, h3.ContainmentCenter)
		if err != nil {
			return nil, err
		}
		for _, c := range cells {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// ringArea is the shoelace sum in degrees²: negative for clockwise rings.
func ringArea(r []pt) float64 {
	var a float64
	for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 { // edge j -> i
		a += (r[j].Lon - r[i].Lon) * (r[j].Lat + r[i].Lat)
	}
	return a / 2
}

func ringContains(r []pt, p pt) bool {
	in := false
	for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
		a, b := r[i], r[j]
		if (a.Lat > p.Lat) != (b.Lat > p.Lat) && p.Lon < (b.Lon-a.Lon)*(p.Lat-a.Lat)/(b.Lat-a.Lat)+a.Lon {
			in = !in
		}
	}
	return in
}

// geoLoop converts a closed ring to an H3 loop (which must not repeat its
// first vertex).
func geoLoop(r []pt) h3.GeoLoop {
	if n := len(r); n > 1 && r[0] == r[n-1] {
		r = r[:n-1]
	}
	loop := make(h3.GeoLoop, len(r))
	for i, p := range r {
		loop[i] = h3.NewLatLng(p.Lat, p.Lon)
	}
	return loop
}

// weightedPercentiles returns a function giving the weighted mid-rank
// percentile (0-100) of a value within vals.
func weightedPercentiles(vals, w []float64) func(float64) float64 {
	idx := make([]int, len(vals))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return vals[idx[a]] < vals[idx[b]] })
	sorted := make([]float64, len(vals))
	cum := make([]float64, len(vals)+1) // cum[i] = weight of the first i values
	for i, k := range idx {
		sorted[i] = vals[k]
		cum[i+1] = cum[i] + w[k]
	}
	total := cum[len(vals)]
	return func(v float64) float64 {
		if total == 0 {
			return 50
		}
		lo := sort.SearchFloat64s(sorted, v)
		hi := sort.Search(len(sorted), func(i int) bool { return sorted[i] > v })
		return 100 * (cum[lo] + 0.5*(cum[hi]-cum[lo])) / total
	}
}

func spainMethod(crimeYear, incomeYear int) []string {
	return []string{
		fmt.Sprintf("Crime: Interior Ministry Balance de Criminalidad, %d full year, types 1–10 (homicide, assault, kidnapping, sexual offences, robbery, burglary, theft, vehicle theft, drug trafficking), per resident. Published only for municipalities over 20,000 residents.", crimeYear),
		"Crime percentile is population-weighted across those municipalities: 30 means 30% of their residents live somewhere with a lower rate. The value is the same for the whole municipality.",
		fmt.Sprintf("Income: INE ADRH %d, net income per person, per census section. Percentile is population-weighted across all of Spain and inverted, so 0 = highest income, 100 = lowest.", incomeYear),
		fmt.Sprintf("Location: each H3 res-%d cell belongs to the census section containing its centre (sections smaller than a cell use res %d). Where every section is at least %d times larger than a coarser cell (countryside), that whole cell goes to the section covering most of it.", esCellRes, esCellRes+1, esCoarseFactor),
	}
}

var spainCaveats = []string{
	"Crime is one number per municipality, and only municipalities over 20,000 residents are published; smaller places have no crime value.",
	"Crime per resident inflates tourist and commuter towns, where many offences involve visitors.",
	"Income is a context indicator, not a measure of street quality or safety; switch it off in the popup if you do not want it in the score.",
	"Data is not live: crime covers the last full year published, income lags about two years.",
}

func writeSpainSummary(path string, idx esIndex, munis map[string]esMuniOut, shardBytes int) error {
	var b strings.Builder
	t := idx.Meta.Totals
	fmt.Fprintf(&b, "# Spain-wide layer\n\nGenerated %s. Crime %s, income %s, sections of %s.\n\n",
		idx.Meta.GeneratedAt, idx.Meta.Ranges["crime"].From[:4], idx.Meta.Ranges["income"].From, idx.Meta.Ranges["sections"].From)
	fmt.Fprintf(&b, "- %d census sections in %d municipalities, %d residents.\n", t["sections"], t["municipalities"], t["population"])
	fmt.Fprintf(&b, "- Crime published for %d municipalities: %d residents (%.0f%%).\n", t["municipalities_with_crime"], t["population_with_crime"], 100*float64(t["population_with_crime"])/float64(t["population"]))
	fmt.Fprintf(&b, "- %d H3 cells in %d shards, %d KB in total.\n\n", t["cells"], len(idx.Shards), shardBytes/1024)

	type row struct {
		code string
		m    esMuniOut
	}
	var rows []row
	for c, m := range munis {
		if m.Crime != nil && m.Pop >= 100000 {
			rows = append(rows, row{c, m})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return *rows[i].m.Per1k < *rows[j].m.Per1k })
	fmt.Fprintf(&b, "## Municipalities over 100,000 residents by street crime per 1,000 residents\n\n| # | municipality | residents | crimes | per 1k | percentile | income €/person |\n|---:|---|---:|---:|---:|---:|---:|\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "| %d | %s (%s) | %d | %d | %.1f | %d | %d |\n", i+1, r.m.Name, r.code, r.m.Pop, *r.m.Count, *r.m.Per1k, *r.m.Crime, r.m.Income)
	}
	logf("wrote %s", path)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

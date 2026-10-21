package main

// Madrid layer: a hex grid like Barcelona's, from Madrid's own open data.
// Dirt and graffiti come from geocoded citizen reports (Avisos Madrid, one
// point each). Incivism and crime come from the Policía Municipal control
// room's incidents, which are published per barrio. Madrid publishes nothing
// on smell, so this grid has no smell layer. Residents and income per census
// section come from INE, as in the Spain-wide layer.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

const (
	madridCKAN       = "https://datos.madrid.es/api/3/action/package_show?id="
	madridAvisosID   = "212411-0-madrid-avisa"
	madridEmisoraID  = "837676-0-incidencias-recibidas-en-la-emisora-central-de-policia-municipal"
	madridBarriosURL = "https://geoportal.madrid.es/fsdescargas/IDEAM_WBGEOPORTAL/LIMITES_ADMINISTRATIVOS/Barrios/Barrios.zip"
	madridMuniCode   = "28079"
)

var (
	madridAvisosRe  = regexp.MustCompile(`^Avisos ciudadanos recibidos \(SIC\)\. (\d{4})$`)
	madridEmisoraRe = regexp.MustCompile(`^Incidencias emisora Polic\S+ Municipal\. (\S+) (\d{4})$`)
	spanishMonths   = map[string]int{"enero": 1, "febrero": 2, "marzo": 3, "abril": 4, "mayo": 5, "junio": 6,
		"julio": 7, "agosto": 8, "septiembre": 9, "octubre": 10, "noviembre": 11, "diciembre": 12}
)

// Avisos categories (CATEGORIA_NIVEL2, normalised) counted as dirt: street
// and green-space cleaning, urgent cleaning (SELUR), bins and containers not
// emptied or abandoned, dumped furniture and rubble, full litter bins. Rats
// count as dirt, as in Barcelona. Requests for new bins, scheduled furniture
// pick-ups and dead animals are left out.
var madridDirt = map[string]bool{
	"limpieza de calles":                            true,
	"limpieza en vias publicas":                     true,
	"limpieza de zonas verdes":                      true,
	"limpieza en zonas verdes":                      true,
	"limpieza en solares municipales":               true,
	"limpieza mobiliario urbano o areas infantiles": true,
	"selur":                              true,
	"vaciado de cubo o contenedor":       true,
	"cubo o contenedor abandonado":       true,
	"muebles abandonados en via publica": true,
	"saco o contenedor de escombros":     true,
	"vaciado de papelera":                true,
	"ratas y cucarachas":                 true,
}

func madridClassify(level1, level2 string) int {
	n1, n2 := norm(level1), norm(level2)
	switch {
	case strings.HasPrefix(n1, "z "), strings.HasPrefix(n1, "zz "):
		return catNone // test and retired categories ("zz[Pruebas Betatester]")
	case n2 == "pintadas y grafitis":
		return catGraffiti
	case madridDirt[n2], n1 == "ratas y cucarachas":
		return catDirt
	}
	return catNone
}

// Emisora Central incident types. Crime: thefts, robberies, burglaries,
// fights and assaults, vehicle theft and "other offences". Incivism: noise,
// drinking or drugs in the street, breaches of the coexistence bylaw, street
// vending and late-night venues. Private disputes and traffic are left out.
var (
	madridCrimeTypes = map[string]bool{
		"robos con fuerza": true, "hurtos": true, "robos con violencia intimidacion": true,
		"reyertas agresiones": true, "sustraccion de vehiculo": true, "otros delitos": true,
	}
	madridIncivismTypes = map[string]bool{
		"ruidos molestos": true, "consumo alcohol drogas en via publica": true, "normas de convivencia": true,
		"venta ambulante": true, "actuaciones advas en locales publicos y de ocio": true,
	}
)

type madridBarrio struct {
	Key            int // district*100 + barrio number, as in Avisos (DISTRITO_ID, BARRIO_ID)
	Name, District string
	Shape          *Shape
}

type madridRaw struct {
	Dirt     int `json:"dirt"`
	Graffiti int `json:"graffiti"`
}

type madridCellOut struct {
	Dirt     int       `json:"dirt"`
	Graffiti int       `json:"graffiti"`
	Incivism int       `json:"incivism"`
	Crime    int       `json:"crime"`
	Score    int       `json:"score"`
	Pop      int       `json:"pop"`
	Barri    int       `json:"barri"`
	Income   int       `json:"inc,omitempty"`
	LowPop   bool      `json:"lp,omitempty"`
	Raw      madridRaw `json:"n_raw"`
}

type madridBarrioOut struct {
	Name       string    `json:"name"`
	District   string    `json:"district"`
	Pop        int       `json:"pop"`
	Dirt       int       `json:"dirt"`
	Graffiti   int       `json:"graffiti"`
	Incivism   int       `json:"incivism"`
	Crime      int       `json:"crime"`
	Score      int       `json:"score"`
	Income     int       `json:"inc,omitempty"`
	CrimeN     int       `json:"crime_n"`
	IncivismN  int       `json:"incivism_n"`
	CrimePer1k float64   `json:"crime_per_1k_year"`
	Raw        madridRaw `json:"n_raw"`
}

type madridGrid struct {
	Meta   Meta                       `json:"meta"`
	Cells  map[string]madridCellOut   `json:"cells"`
	Barris map[string]madridBarrioOut `json:"barris"`
}

var madridCats = []string{"dirt", "graffiti", "incivism", "crime"}

// Barcelona's default weights without smell (the extension uses the popup's
// weights; these only give the score stored in the file).
var madridWeights = map[string]float64{"crime": 0.35, "dirt": 0.2, "graffiti": 0.15, "incivism": 0.1}

func runMadrid(f *fetcher, outDir string, months, res int) error {
	var sources []Source
	ranges := map[string]Range{}

	// --- barrios, sections, residents ---
	bp, err := f.get(madridBarriosURL, "madrid_barrios.zip")
	if err != nil {
		return err
	}
	sources = append(sources, Source{Portal: "Geoportal Madrid", Dataset: "300496-0-barrios-madrid", Title: "Barrios municipales de Madrid", Resource: "Barrios.zip (SHP, ETRS89 UTM 30)", URL: madridBarriosURL})
	recs, err := readZippedShapefile(bp, func(x, y float64) pt { return utmToLonLat(x, y, utmZone30CM) })
	if err != nil {
		return err
	}
	barrios := map[int]*madridBarrio{}
	byName := map[string]int{}
	for _, r := range recs {
		b := &madridBarrio{Key: atoi(r.Attrs["CODDIS"])*100 + atoi(r.Attrs["NUM_BAR"]), Name: r.Attrs["NOMBRE"], District: r.Attrs["NOMDIS"], Shape: r.Shape}
		barrios[b.Key] = b
		byName[norm(r.Attrs["BARRIO_MAY"])] = b.Key
		byName[norm(b.Name)] = b.Key
	}
	if len(barrios) < 120 {
		return fmt.Errorf("Madrid barrios: only %d polygons", len(barrios))
	}

	adrh, err := loadADRH(f)
	if err != nil {
		return err
	}
	sources = append(sources, adrh.Sources...)
	ranges["population"] = Range{fmt.Sprint(adrh.Year), fmt.Sprint(adrh.Year)}
	ranges["income"] = ranges["population"]
	esSecs, secYear, src, err := loadINESections(f, adrh)
	if err != nil {
		return err
	}
	sources = append(sources, src)
	secs := map[int]*Section{}
	var unplaced int
	for _, s := range esSecs {
		if s.Muni != madridMuniCode {
			continue
		}
		// A point surely inside the section decides its barrio (sections nest
		// in barrios, but polygon centroids can fall outside odd shapes).
		p := s.Shape.Centroid()
		if cells, err := shapeCells(s.Shape, 10); err == nil && len(cells) > 0 {
			if ll, err := cells[len(cells)/2].LatLng(); err == nil {
				p = pt{Lon: ll.Lng, Lat: ll.Lat}
			}
		}
		key := 0
		for _, b := range barrios {
			if b.Shape.Contains(p.Lon, p.Lat) {
				key = b.Key
				break
			}
		}
		if key == 0 {
			unplaced++
		}
		secs[atoi(s.Code[5:])] = &Section{Key: atoi(s.Code[5:]), Barri: key, Shape: s.Shape, Pop: s.Pop, Income: s.Income}
	}
	logf("Madrid: %d census sections (boundaries of %d, residents and income of %d), %d without a barrio", len(secs), secYear, adrh.Year, unplaced)
	hexes, err := distributePopulation(secs, res)
	if err != nil {
		return err
	}
	barrioPop := map[int]float64{}
	var totalPop float64
	for _, s := range secs {
		barrioPop[s.Barri] += s.Pop
		totalPop += s.Pop
	}
	logf("Madrid: %.0f residents, H3 r%d: %d cells", totalPop, res, len(hexes))

	// --- citizen reports: dirt, graffiti ---
	pkg, err := madridPackage(f, madridAvisosID)
	if err != nil {
		return err
	}
	type yearRes struct {
		year int
		res  ckanResource
	}
	var years []yearRes
	for _, r := range pkg.Resources {
		if m := madridAvisosRe.FindStringSubmatch(r.Description); m != nil && strings.EqualFold(r.Format, "CSV") {
			years = append(years, yearRes{atoi(m[1]), r.ckanResource})
		}
	}
	sort.Slice(years, func(i, j int) bool { return years[i].year > years[j].year })
	need := min(len(years), (months+11)/12+1)
	if need == 0 {
		return fmt.Errorf("Avisos Madrid: no yearly CSV of received reports")
	}
	var avisos []madridAviso
	byMonth := map[month]int{}
	maxDay := map[month]int{}
	var st madridAvisoStats
	for _, y := range years[:need] {
		p, err := f.get(y.res.URL, fmt.Sprintf("madrid_avisos_%d_%s", y.year, path.Base(y.res.URL)))
		if err != nil {
			return err
		}
		sources = append(sources, Source{Portal: "Datos abiertos Ayuntamiento de Madrid", Dataset: madridAvisosID, Title: pkg.Title, Resource: fmt.Sprintf("Avisos ciudadanos recibidos (SIC). %d", y.year), URL: y.res.URL, Modified: pkg.Modified})
		if err := loadMadridAvisos(p, &avisos, byMonth, maxDay, &st); err != nil {
			return err
		}
	}
	var to month
	for m, d := range maxDay {
		if d >= 28 && m > to {
			to = m
		}
	}
	from := to - month(months-1)
	if from < mkMonth(years[need-1].year, 1) {
		return fmt.Errorf("Avisos Madrid window starts %s, before the oldest downloaded file", from)
	}
	ranges["avisos"] = Range{from.String(), to.String()}
	mapping := map[[3]string]*mapRow{}
	var outside, inBarrio, withBarrio int
	totals := map[string]int{}
	for _, a := range avisos {
		if a.Month < from || a.Month > to {
			continue
		}
		k := [3]string{a.Level1, a.Level2, ""}
		mr := mapping[k]
		if mr == nil {
			mr = &mapRow{Area: a.Level1, Element: a.Level2, Cat: madridClassify(a.Level1, a.Level2)}
			mapping[k] = mr
		}
		mr.N++
		// Datum check: the point should lie in the barrio the record names.
		if b := barrios[a.Barrio]; b != nil {
			withBarrio++
			if b.Shape.Contains(a.Lon, a.Lat) {
				inBarrio++
			}
		}
		if mr.Cat == catNone {
			continue
		}
		cell, err := h3.LatLngToCell(h3.NewLatLng(a.Lat, a.Lon), res)
		if err != nil {
			return err
		}
		h := hexes[cell]
		if h == nil {
			outside++
			continue
		}
		h.Counts[mr.Cat]++
		totals[irisCatNames[mr.Cat]]++
	}
	share := float64(inBarrio) / float64(max(withBarrio, 1))
	logf("Avisos Madrid: %d rows, %d reports (Aviso) with coordinates; window %s..%s; %.1f%% of points inside the barrio they name; %d mapped reports outside the city cells",
		st.Rows, len(avisos), from, to, 100*share, outside)
	if share < 0.9 {
		return fmt.Errorf("Avisos Madrid: only %.1f%% of points fall in their own barrio: wrong datum or projection?", 100*share)
	}
	totals["avisos_dropped_outside"] = outside

	// --- Policía Municipal control room: incivism, crime per barrio ---
	crimeN, incivismN, emFrom, emTo, emSrc, err := loadMadridEmisora(f, byName)
	if err != nil {
		return err
	}
	sources = append(sources, emSrc...)
	ranges["police_incidents"] = Range{emFrom.String(), emTo.String()}
	emYears := float64(emTo-emFrom+1) / 12
	var crimeTot, incTot float64
	for _, v := range crimeN {
		crimeTot += v
	}
	for _, v := range incivismN {
		incTot += v
	}
	totals["crime_incidents"], totals["incivism_incidents"] = round(crimeTot), round(incTot)

	// --- rates, percentiles, scores ---
	pcts := map[string]map[h3.Cell]float64{
		"dirt":     percentiles(hexes, hexRates(hexes, catDirt)),
		"graffiti": percentiles(hexes, hexRates(hexes, catGraffiti)),
	}
	crimeRate, incRate := barriCrimeRates(crimeN, barrioPop), barriCrimeRates(incivismN, barrioPop)
	cv, iv := map[h3.Cell]float64{}, map[h3.Cell]float64{}
	for c, h := range hexes {
		cv[c], iv[c] = crimeRate[h.Barri], incRate[h.Barri]
	}
	pcts["crime"], pcts["incivism"] = percentiles(hexes, cv), percentiles(hexes, iv)

	grid := madridGrid{Cells: map[string]madridCellOut{}, Barris: map[string]madridBarrioOut{}}
	type acc struct {
		pop, inc, incPop float64
		pct              map[string]float64
		raw              [2]float64
	}
	bacc := map[int]*acc{}
	for c, h := range hexes {
		p := map[string]float64{}
		for _, k := range madridCats {
			p[k] = pcts[k][c]
		}
		grid.Cells[c.String()] = madridCellOut{
			Dirt: round(p["dirt"]), Graffiti: round(p["graffiti"]), Incivism: round(p["incivism"]), Crime: round(p["crime"]),
			Score: round(score(p, madridWeights)), Pop: round(h.Pop), Barri: h.Barri, Income: round(h.Income()),
			LowPop: h.Pop < residentialMinPop, Raw: madridRaw{round(h.Counts[catDirt]), round(h.Counts[catGraffiti])},
		}
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
			for _, k := range madridCats {
				a.pct[k] += p[k] * w
			}
			a.raw[0] += h.Counts[catDirt] * w / wsum
			a.raw[1] += h.Counts[catGraffiti] * w / wsum
		}
	}
	for key, b := range barrios {
		a := bacc[key]
		if a == nil || a.pop == 0 {
			continue
		}
		p := map[string]float64{}
		for _, k := range madridCats {
			p[k] = a.pct[k] / a.pop
		}
		bo := madridBarrioOut{
			Name: b.Name, District: b.District, Pop: round(barrioPop[key]),
			Dirt: round(p["dirt"]), Graffiti: round(p["graffiti"]), Incivism: round(p["incivism"]), Crime: round(p["crime"]),
			Score: round(score(p, madridWeights)), CrimeN: round(crimeN[key]), IncivismN: round(incivismN[key]),
			Raw: madridRaw{round(a.raw[0]), round(a.raw[1])},
		}
		if barrioPop[key] > 0 {
			bo.CrimePer1k = float64(round(10*crimeN[key]/barrioPop[key]*1000/emYears)) / 10
		}
		if a.incPop > 0 {
			bo.Income = round(a.inc / a.incPop)
		}
		grid.Barris[fmt.Sprint(key)] = bo
	}
	totals["cells"] = len(grid.Cells)
	totals["population"] = round(totalPop)
	grid.Meta = Meta{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		H3Resolution: res,
		Categories:   madridCats,
		Weights:      madridWeights,
		Ranges:       ranges,
		Sources:      sources,
		Totals:       totals,
		Method:       madridMethod(months),
		Caveats:      madridCaveats,
	}

	dir := filepath.Join(outDir, "madrid")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "grid.json"), grid); err != nil {
		return err
	}
	if err := writeMadridMapping(filepath.Join(dir, "mapping.md"), mapping, from, to); err != nil {
		return err
	}
	return writeMadridRankings(filepath.Join(dir, "rankings.md"), grid)
}

// madridPackage reads CKAN metadata from datos.madrid.es (always fresh, so
// new monthly and yearly files are seen).
func madridPackage(f *fetcher, id string) (*madridCKANPackage, error) {
	raw, err := f.fresh(madridCKAN + id)
	if err != nil {
		return nil, err
	}
	var body struct {
		Success bool              `json:"success"`
		Result  madridCKANPackage `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || !body.Success {
		return nil, fmt.Errorf("datos.madrid.es package %s: %v", id, err)
	}
	return &body.Result, nil
}

type madridCKANPackage struct {
	Title     string               `json:"title"`
	Modified  string               `json:"metadata_modified"`
	Resources []madridCKANResource `json:"resources"`
}

type madridCKANResource struct {
	ckanResource
	Description string `json:"description"`
}

type madridAviso struct {
	Month          month
	Lat, Lon       float64
	Level1, Level2 string
	Barrio         int
}

type madridAvisoStats struct{ Rows, Requests, NoCoords int }

// loadMadridAvisos reads one yearly file of received reports. Only "Aviso"
// records (reports of a problem) are kept; "Petición" records are requests.
// Dates are dd/mm/yyyy, despite the documentation's mm/dd/yyyy.
func loadMadridAvisos(p string, out *[]madridAviso, byMonth, maxDay map[month]int, st *madridAvisoStats) error {
	fh, err := os.Open(p)
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
		return fmt.Errorf("%s: %w", p, err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimPrefix(strings.TrimSpace(h), "\uFEFF")] = i
	}
	c, err := col(idx, p, "TIPO_INCIDENCIA", "FECHA_DE_RECEPCION", "CATEGORIA_NIVEL1", "CATEGORIA_NIVEL2",
		"DISTRITO_ID", "BARRIO_ID", "COORDENADA_OFICIAL_X", "COORDENADA_OFICIAL_Y")
	if err != nil {
		return err
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if len(rec) <= c[7] {
			continue
		}
		st.Rows++
		if !strings.EqualFold(strings.TrimSpace(rec[c[0]]), "Aviso") {
			st.Requests++
			continue
		}
		x, err1 := strconv.ParseFloat(strings.TrimSpace(rec[c[6]]), 64)
		y, err2 := strconv.ParseFloat(strings.TrimSpace(rec[c[7]]), 64)
		if err1 != nil || err2 != nil || x < 400000 || y < 4400000 {
			st.NoCoords++
			continue
		}
		d := strings.Split(strings.TrimSpace(rec[c[1]]), "/")
		if len(d) != 3 {
			continue
		}
		day, mon, yr := atoi(d[0]), atoi(d[1]), atoi(d[2])
		if mon < 1 || mon > 12 || day < 1 || day > 31 {
			continue
		}
		m := mkMonth(yr, mon)
		byMonth[m]++
		maxDay[m] = max(maxDay[m], day)
		q := utmToLonLat(x, y, utmZone30CM)
		*out = append(*out, madridAviso{Month: m, Lat: q.Lat, Lon: q.Lon, Level1: rec[c[2]], Level2: rec[c[3]],
			Barrio: atoi(rec[c[4]])*100 + atoi(rec[c[5]])})
	}
}

// loadMadridEmisora sums the control room's monthly incidents per barrio
// (all months published) into crime and incivism counts.
func loadMadridEmisora(f *fetcher, byName map[string]int) (crime, incivism map[int]float64, from, to month, sources []Source, err error) {
	pkg, err := madridPackage(f, madridEmisoraID)
	if err != nil {
		return
	}
	crime, incivism = map[int]float64{}, map[int]float64{}
	unmatched := map[string]float64{}
	seen := map[month]bool{}
	for _, r := range pkg.Resources {
		m := madridEmisoraRe.FindStringSubmatch(strings.TrimSpace(r.Description))
		if m == nil || !strings.EqualFold(r.Format, "CSV") {
			continue
		}
		mon, ok := spanishMonths[strings.ToLower(m[1])]
		if !ok {
			continue
		}
		mm := mkMonth(atoi(m[2]), mon)
		if seen[mm] {
			continue
		}
		seen[mm] = true
		var p string
		if p, err = f.get(r.URL, fmt.Sprintf("madrid_emisora_%s_%s", mm, path.Base(r.URL))); err != nil {
			return
		}
		if err = readMadridEmisora(p, byName, crime, incivism, unmatched); err != nil {
			return
		}
		if from == 0 || mm < from {
			from = mm
		}
		to = max(to, mm)
	}
	if len(seen) == 0 {
		err = fmt.Errorf("Emisora Policía Municipal: no monthly CSV")
		return
	}
	if len(unmatched) > 0 {
		logf("Emisora: incidents in barrios without a polygon (ignored): %v", unmatched)
	}
	logf("Emisora: %d months %s..%s", len(seen), from, to)
	sources = append(sources, Source{Portal: "Datos abiertos Ayuntamiento de Madrid", Dataset: madridEmisoraID, Title: pkg.Title,
		Resource: fmt.Sprintf("%d monthly CSVs, %s … %s", len(seen), from, to), URL: madridCKAN + madridEmisoraID, Modified: pkg.Modified})
	return
}

func readMadridEmisora(p string, byName map[string]int, crime, incivism map[int]float64, unmatched map[string]float64) error {
	fh, err := os.Open(p)
	if err != nil {
		return err
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if _, err := r.Read(); err != nil { // Día;Hora;Distrito;Barrio;Origen;Incidentes;tipo
		return fmt.Errorf("%s: %w", p, err)
	}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if len(rec) < 7 {
			continue
		}
		t := norm(rec[6])
		var dst map[int]float64
		switch {
		case madridCrimeTypes[t]:
			dst = crime
		case madridIncivismTypes[t]:
			dst = incivism
		default:
			continue
		}
		n, _ := strconv.ParseFloat(strings.TrimSpace(rec[5]), 64)
		key, ok := byName[norm(rec[3])]
		if !ok {
			unmatched[rec[3]] += n
			continue
		}
		dst[key] += n
	}
}

func madridMethod(months int) []string {
	return []string{
		fmt.Sprintf("Dirt and graffiti: Avisos Madrid citizen reports (type 'Aviso', not requests) at the official address coordinate, last %d full months, mapped by category; per resident with first-ring smoothing and shrinkage toward the city rate, as in Barcelona.", months),
		"Residents per H3 cell: INE census-section population spread evenly over each section (20 m lattice).",
		"Incivism and crime: Policía Municipal control-room (Emisora Central) incidents per barrio, every month published; per resident, shrunk toward the city rate (2,000 residents). Every hex takes its barrio's rate.",
		fmt.Sprintf("Values are city-wide percentiles (0 = best, 100 = worst) against cells with ≥%d residents. Madrid publishes no smell data, so the score leaves smell out.", residentialMinPop),
	}
}

var madridCaveats = []string{
	"Reports measure reporting, not reality: dense and central areas report more, and some neighbourhoods rarely use Avisos Madrid.",
	"Crime here is incidents handled by the city police (Policía Municipal), not recorded crime: most offences in Madrid are reported to the Policía Nacional, whose figures exist only for the whole city.",
	"Crime and incivism are per barrio (131 barrios), not per hex. Noise includes complaints about neighbours' homes.",
	"Normalising by residents inflates areas with few residents and many visitors (Centro, business districts, parks).",
	"There is no smell layer in Madrid: the city publishes no data on it.",
}

func writeMadridMapping(p string, mapping map[[3]string]*mapRow, from, to month) error {
	rows := make([]*mapRow, 0, len(mapping))
	tot := map[int]int{}
	for _, r := range mapping {
		rows = append(rows, r)
		tot[r.Cat] += r.N
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].N != rows[j].N {
			return rows[i].N > rows[j].N
		}
		return rows[i].Area+rows[i].Element < rows[j].Area+rows[j].Element
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# Avisos Madrid → category mapping (%s … %s)\n\nReports of type 'Aviso' with coordinates. Rules: `madridClassify` in `pipeline/madrid.go`.\n\n| category | records |\n|---|---:|\n", from, to)
	for _, c := range []int{catDirt, catGraffiti, catNone} {
		fmt.Fprintf(&b, "| %s | %d |\n", catLabel(c), tot[c])
	}
	for _, c := range []int{catDirt, catGraffiti, catNone} {
		fmt.Fprintf(&b, "\n## %s\n\n| records | CATEGORIA_NIVEL1 | CATEGORIA_NIVEL2 |\n|---:|---|---|\n", catLabel(c))
		for _, r := range rows {
			if r.Cat == c {
				fmt.Fprintf(&b, "| %d | %s | %s |\n", r.N, r.Area, r.Element)
			}
		}
	}
	logf("wrote %s", p)
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

func writeMadridRankings(p string, g madridGrid) error {
	type row struct {
		key string
		madridBarrioOut
	}
	var all []row
	for _, k := range sortedKeys(g.Barris) {
		all = append(all, row{k, g.Barris[k]})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	var b strings.Builder
	table := func(title string, rows []row, start int) {
		fmt.Fprintf(&b, "\n## %s\n\n| # | barrio | district | residents | score | dirt | graffiti | incivism | crime | crime incidents/1k/yr | income €/p |\n|---:|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n", title)
		for i, r := range rows {
			fmt.Fprintf(&b, "| %d | %s | %s | %d | **%d** | %d | %d | %d | %d | %.1f | %d |\n",
				start+i, r.Name, r.District, r.Pop, r.Score, r.Dirt, r.Graffiti, r.Incivism, r.Crime, r.CrimePer1k, r.Income)
		}
	}
	fmt.Fprintf(&b, "# Madrid barrio rankings (default weights; percentiles 0 = best, 100 = worst)\n")
	n := min(20, len(all))
	table("Top 20 (best score)", all[:n], 1)
	table("Bottom 20 (worst score)", all[len(all)-n:], len(all)-n+1)
	dist := map[string][2]float64{}
	for _, r := range all {
		d := dist[r.District]
		dist[r.District] = [2]float64{d[0] + float64(r.Score*r.Pop), d[1] + float64(r.Pop)}
	}
	fmt.Fprintf(&b, "\n## Districts (population-weighted barrio score)\n\n| district | score |\n|---|---:|\n")
	names := sortedKeys(dist)
	sort.SliceStable(names, func(i, j int) bool { return dist[names[i]][0]/dist[names[i]][1] > dist[names[j]][0]/dist[names[j]][1] })
	for _, d := range names {
		fmt.Fprintf(&b, "| %s | %.0f |\n", d, dist[d][0]/dist[d][1])
	}
	logf("wrote %s", p)
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

// The panel tells people "complaints here (2024-04 – 2026-03): dirt 833,
// smell 30, graffiti 154, bad behaviour 61". These tests pin every step from
// the IRIS rows to those numbers: which rows count, which month, which hex,
// which category, and that n_raw is the plain count, not a smoothed rate.

var irisHeader = []string{"FITXA_ID", "TIPUS", "AREA", "ELEMENT", "DETALL", "DIA_DATA_ALTA", "MES_DATA_ALTA", "ANY_DATA_ALTA",
	"DIA_DATA_TANCAMENT", "MES_DATA_TANCAMENT", "ANY_DATA_TANCAMENT", "LONGITUD", "LATITUD"}

// irisRow is one CSV row; opened and closed are "YYYY-MM".
type irisRow struct {
	id, typ, area, element, detail, opened, closed, lat, lon string
}

func (r irisRow) fields() []string {
	return []string{r.id, r.typ, r.area, r.element, r.detail, "15", r.opened[5:], r.opened[:4], "20", r.closed[5:], r.closed[:4], r.lon, r.lat}
}

func writeIRIS(t *testing.T, name string, bom bool, rows ...irisRow) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if bom {
		f.WriteString("\xEF\xBB\xBF") // UTF-8 byte order mark
	}
	w := csv.NewWriter(f)
	w.Write(irisHeader)
	for _, r := range rows {
		w.Write(r.fields())
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

const (
	areaClean   = "Recollida i neteja de l'espai urbà"
	areaSafety  = "Prevenció i seguretat"
	elStreet    = "Neteja de l'espai públic"
	elGraffiti  = "Pintades / cartells / pancartes"
	elPublicOrd = "Incompliment d'ordenances a l'espai públic"
)

// One AREA/ELEMENT/DETALL triple per category, taken from the real files.
var (
	dirtRow     = irisRow{area: areaClean, element: elStreet, detail: "Objectes a netejar / retirar"}
	smellRow    = irisRow{area: areaClean, element: elStreet, detail: "Orins, excrements animals"}
	graffitiRow = irisRow{area: areaClean, element: elGraffiti, detail: "Netejar pintada  cartell o pancarta en espai públic"}
	incivismRow = irisRow{area: areaSafety, element: elPublicOrd, detail: "Drogues"}
	noneRow     = irisRow{area: "Manteniment de l'espai urbà", element: "Enllumenat públic", detail: "Enllumenat incidències"}
)

// Plaça de Catalunya.
const pcLat, pcLon = 41.3870, 2.1700

func row(base irisRow, id, typ, opened string, lat, lon float64) irisRow {
	base.id, base.typ, base.opened, base.closed = id, typ, opened, opened
	base.lat, base.lon = fmt.Sprintf("%.6f", lat), fmt.Sprintf("%.6f", lon)
	return base
}

func TestLoadIRISKeepsOnlyComplaints(t *testing.T) {
	ok := []string{"INCIDENCIA", "QUEIXA", "INCIDÈNCIA", " Incidència ", "incidencia", "ISSUE", "COMPLAINT", "Queixa"}
	notOk := []string{"CONSULTA", "SUGGERIMENT", "PETICIO DE SERVEI", "AGRAIMENT", "QUERY", "SUGGESTION", "SERVICE REQUEST", "GRATITUDE", ""}
	var rows []irisRow
	for i, typ := range append(ok, notOk...) {
		rows = append(rows, row(dirtRow, fmt.Sprint(100+i), typ, "2025-06", pcLat, pcLon))
	}
	recs, st, err := loadIRIS([]string{writeIRIS(t, "iris_2025.csv", false, rows...)})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != len(ok) || st.WrongType != len(notOk) {
		t.Fatalf("kept %d, wrong type %d; want %d and %d", len(recs), st.WrongType, len(ok), len(notOk))
	}
}

func TestLoadIRISDropsUnusableRows(t *testing.T) {
	good := row(dirtRow, "1", "INCIDENCIA", "2025-06", pcLat, pcLon)
	noLat := good
	noLat.id, noLat.lat = "2", ""
	badLon := good
	badLon.id, badLon.lon = "3", "2,170000" // decimal comma: not a number
	madrid := row(dirtRow, "4", "INCIDENCIA", "2025-06", 40.4168, -3.7038)
	zero := row(dirtRow, "5", "INCIDENCIA", "2025-06", 0, 0)
	sea := row(dirtRow, "6", "INCIDENCIA", "2025-06", 41.30, 2.20) // south of the bounding box
	query := row(dirtRow, "7", "CONSULTA", "2025-06", 40.4168, -3.7038)

	recs, st, err := loadIRIS([]string{writeIRIS(t, "iris_2025.csv", false, good, noLat, badLon, madrid, zero, sea, query)})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || st.NoCoords != 2 || st.OutsideBCN != 3 || st.WrongType != 1 || st.Rows != 7 {
		t.Fatalf("kept %d, stats %+v", len(recs), st)
	}
}

// The source files repeat some rows verbatim. Each FITXA_ID is one request
// and must be counted once, whether the copy sits in the same yearly file
// or in another one.
func TestLoadIRISCountsEachRequestOnce(t *testing.T) {
	a := row(dirtRow, "38755908", "INCIDENCIA", "2025-11", pcLat, pcLon)
	b := row(smellRow, "38755913", "QUEIXA", "2025-12", pcLat, pcLon)
	c := row(graffitiRow, "49201212", "INCIDÈNCIA", "2025-12", pcLat, pcLon)
	f2025 := writeIRIS(t, "iris_2025.csv", false, a, b, a, a)
	f2026 := writeIRIS(t, "iris_2026.csv", false, b, c)

	recs, st, err := loadIRIS([]string{f2026, f2025})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || st.Duplicates != 3 || st.Rows != 6 {
		t.Fatalf("kept %d, duplicates %d, rows %d; want 3, 3, 6", len(recs), st.Duplicates, st.Rows)
	}
	if n := st.ByMonth[mkMonth(2025, 12)]; n != 2 {
		t.Errorf("2025-12 has %d records, want 2", n)
	}

	// A duplicate dropped before the type check must not hide a different
	// request: rows without an id are all kept.
	noID := row(dirtRow, "", "INCIDENCIA", "2025-06", pcLat, pcLon)
	recs, st, err = loadIRIS([]string{writeIRIS(t, "iris_2025.csv", false, noID, noID)})
	if err != nil || len(recs) != 2 || st.Duplicates != 0 {
		t.Fatalf("rows without FITXA_ID: kept %d, duplicates %d, err %v", len(recs), st.Duplicates, err)
	}
}

// Every row ends up in exactly one bucket, so nothing is lost or invented.
func TestLoadIRISStatsAddUp(t *testing.T) {
	rows := []irisRow{
		row(dirtRow, "1", "INCIDENCIA", "2025-01", pcLat, pcLon),
		row(dirtRow, "1", "INCIDENCIA", "2025-01", pcLat, pcLon),
		row(dirtRow, "2", "CONSULTA", "2025-01", pcLat, pcLon),
		row(dirtRow, "3", "QUEIXA", "2025-01", 0, 0),
		row(noneRow, "4", "QUEIXA", "2025-02", pcLat, pcLon),
	}
	noCoord := rows[0]
	noCoord.id, noCoord.lon = "5", " "
	rows = append(rows, noCoord)
	recs, st, err := loadIRIS([]string{writeIRIS(t, "iris_2025.csv", true, rows...)})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(recs) + st.Duplicates + st.WrongType + st.NoCoords + st.OutsideBCN; got != st.Rows {
		t.Fatalf("%d kept + %+v does not add up to %d rows", len(recs), st, st.Rows)
	}
	var byMonth int
	for _, n := range st.ByMonth {
		byMonth += n
	}
	if byMonth != len(recs) {
		t.Fatalf("ByMonth holds %d records, %d were kept", byMonth, len(recs))
	}
}

// The month is when the request was opened, not when it was closed: the
// yearly files are split by closing date.
func TestLoadIRISUsesOpeningMonth(t *testing.T) {
	r := row(dirtRow, "30954661", "INCIDENCIA", "2023-12", 41.379247, 2.152407)
	r.closed = "2024-01"
	recs, _, err := loadIRIS([]string{writeIRIS(t, "iris_2024.csv", true, r)})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("kept %d records", len(recs))
	}
	got := recs[0]
	if got.Month != mkMonth(2023, 12) || got.Lat != 41.379247 || got.Lon != 2.152407 {
		t.Fatalf("got month %s at (%v, %v)", got.Month, got.Lat, got.Lon)
	}
	if got.Area != dirtRow.area || got.Element != dirtRow.element || got.Detail != dirtRow.detail {
		t.Fatalf("text fields changed: %+v", got)
	}
}

func TestLoadIRISMissingColumn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "iris.csv")
	os.WriteFile(p, []byte("FITXA_ID,TIPUS,AREA\n1,QUEIXA,x\n"), 0o644)
	if _, _, err := loadIRIS([]string{p}); err == nil || !strings.Contains(err.Error(), "missing column") {
		t.Fatalf("got %v, want a missing-column error", err)
	}
}

func TestMonthWindow(t *testing.T) {
	to := mkMonth(2026, 3)
	from := to - month(24-1)
	if from != mkMonth(2024, 4) || from.String() != "2024-04" || to.String() != "2026-03" {
		t.Fatalf("24 months ending 2026-03 start at %s", from)
	}
	if mkMonth(2025, 1)-1 != mkMonth(2024, 12) || mkMonth(2024, 12).String() != "2024-12" || mkMonth(2025, 1).String() != "2025-01" {
		t.Fatal("year boundary")
	}
}

func TestLatestFullMonth(t *testing.T) {
	byMonth := map[month]int{
		mkMonth(2026, 1): 21084,
		mkMonth(2026, 2): 23168,
		mkMonth(2026, 3): 1000, // exactly the threshold: counts
		mkMonth(2026, 4): 999,  // stray records: do not move the window
		mkMonth(2027, 1): 3,
	}
	if got := latestFullMonth(byMonth, 1000); got != mkMonth(2026, 3) {
		t.Fatalf("got %s, want 2026-03", got)
	}
	if got := latestFullMonth(map[month]int{}, 1000); got != 0 {
		t.Fatalf("empty: got %s", got)
	}
}

// testHexes returns the hex around Plaça de Catalunya, one of its neighbours,
// and a point inside the city box whose hex is not in the set.
func testHexes(t *testing.T) (map[h3.Cell]*Hex, h3.Cell, h3.Cell, h3.LatLng, h3.LatLng) {
	t.Helper()
	a, err := h3.LatLngToCell(h3.NewLatLng(pcLat, pcLon), 9)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := h3.GridDisk(a, 1)
	if err != nil {
		t.Fatal(err)
	}
	b := disk[0]
	if b == a {
		b = disk[1]
	}
	bll, err := b.LatLng()
	if err != nil {
		t.Fatal(err)
	}
	far := h3.NewLatLng(41.45, 2.10)
	hexes := map[h3.Cell]*Hex{a: {Cell: a, Pop: 1500}, b: {Cell: b, Pop: 800}}
	if c, _ := h3.LatLngToCell(far, 9); hexes[c] != nil {
		t.Fatal("far point falls in a test hex")
	}
	return hexes, a, b, bll, far
}

func rec(base irisRow, m month, ll h3.LatLng) irisRecord {
	return irisRecord{Month: m, Lat: ll.Lat, Lon: ll.Lng, Area: base.area, Element: base.element, Detail: base.detail}
}

func TestCountIRIS(t *testing.T) {
	hexes, a, b, bll, far := testHexes(t)
	pc := h3.NewLatLng(pcLat, pcLon)
	from, to := mkMonth(2024, 4), mkMonth(2026, 3)
	var recs []irisRecord
	add := func(n int, base irisRow, m month, ll h3.LatLng) {
		for range n {
			recs = append(recs, rec(base, m, ll))
		}
	}
	add(1, dirtRow, from, pc)   // first month: in
	add(1, dirtRow, to, pc)     // last month: in
	add(5, dirtRow, from-1, pc) // 2024-03: out
	add(7, dirtRow, to+1, pc)   // 2026-04: out
	add(3, smellRow, mkMonth(2025, 1), pc)
	add(2, graffitiRow, mkMonth(2025, 2), pc)
	add(1, incivismRow, mkMonth(2025, 3), pc)
	add(9, noneRow, mkMonth(2025, 4), pc) // street lights: no category
	add(4, dirtRow, mkMonth(2025, 5), bll)
	add(6, graffitiRow, mkMonth(2025, 6), far) // in no populated hex

	mapping, totals, outside, err := countIRIS(recs, from, to, hexes, 9)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hexes[a].Counts, [nIris]float64{2, 3, 2, 1}; got != want {
		t.Errorf("hex A counts %v, want %v", got, want)
	}
	if got, want := hexes[b].Counts, [nIris]float64{4, 0, 0, 0}; got != want {
		t.Errorf("hex B counts %v, want %v", got, want)
	}
	if outside != 6 {
		t.Errorf("outside %d, want 6", outside)
	}
	wantTotals := map[string]int{"dirt": 6, "smell": 3, "graffiti": 2, "incivism": 1}
	for k, v := range wantTotals {
		if totals[k] != v {
			t.Errorf("totals[%s] = %d, want %d", k, totals[k], v)
		}
	}
	if len(totals) != len(wantTotals) {
		t.Errorf("totals %v has extra keys", totals)
	}

	// The mapping report sees every in-window record, mapped or not.
	key := func(r irisRow) [3]string { return [3]string{r.area, r.element, r.detail} }
	for _, c := range []struct {
		r    irisRow
		n    int
		want int
	}{{dirtRow, 6, catDirt}, {smellRow, 3, catSmell}, {graffitiRow, 8, catGraffiti}, {incivismRow, 1, catIncivism}, {noneRow, 9, catNone}} {
		mr := mapping[key(c.r)]
		if mr == nil || mr.N != c.n || mr.Cat != c.want {
			t.Errorf("mapping %q: %+v, want N=%d cat=%s", c.r.detail, mr, c.n, catLabel(c.want))
		}
	}

	// Conservation: what the hexes hold is exactly what the totals say.
	var sum [nIris]float64
	for _, h := range hexes {
		for i, v := range h.Counts {
			sum[i] += v
		}
	}
	for i, name := range irisCatNames {
		if int(sum[i]) != totals[name] {
			t.Errorf("%s: hexes hold %v, totals say %d", name, sum[i], totals[name])
		}
	}
}

// A record lands in the hex the extension will look up for that point, even
// right next to a hex edge.
func TestCountIRISUsesPointHex(t *testing.T) {
	hexes, a, b, bll, _ := testHexes(t)
	for i := range 20 {
		f := float64(i) / 19 // walk from A's centre to B's centre
		ll := h3.NewLatLng(pcLat+(bll.Lat-pcLat)*f, pcLon+(bll.Lng-pcLon)*f)
		c, _ := h3.LatLngToCell(ll, 9)
		if hexes[c] == nil {
			t.Fatalf("step %d: point left both hexes", i)
		}
		for _, h := range hexes {
			h.Counts = [nIris]float64{}
		}
		if _, _, outside, err := countIRIS([]irisRecord{rec(dirtRow, mkMonth(2025, 1), ll)}, mkMonth(2025, 1), mkMonth(2025, 1), hexes, 9); err != nil || outside != 0 {
			t.Fatalf("step %d: outside %d err %v", i, outside, err)
		}
		if hexes[c].Counts[catDirt] != 1 || hexes[a].Counts[catDirt]+hexes[b].Counts[catDirt] != 1 {
			t.Fatalf("step %d: point in %s, counts A=%v B=%v", i, c, hexes[a].Counts, hexes[b].Counts)
		}
	}
}

// n_raw is what people read as "dirt 833, smell 30, graffiti 154, bad
// behaviour 61": the hex's own counts, each under its own name, untouched by
// the neighbour smoothing and shrinkage that feed the bars.
func TestCellOutRawCounts(t *testing.T) {
	hexes, a, b, _, _ := testHexes(t)
	hexes[a].Counts = [nIris]float64{833, 30, 154, 61}
	hexes[b].Counts = [nIris]float64{5000, 900, 2000, 700}
	for cat := 0; cat < nIris; cat++ {
		hexRates(hexes, cat)
	}
	if hexes[a].Counts != [nIris]float64{833, 30, 154, 61} {
		t.Fatalf("hexRates changed the counts: %v", hexes[a].Counts)
	}
	p := map[string]float64{"dirt": 91.2, "smell": 12.4, "graffiti": 77.7, "incivism": 55.5, "crime": 60}
	b2, err := json.Marshal(cellOut(hexes[a], p))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Dirt int            `json:"dirt"`
		Raw  map[string]int `json:"n_raw"`
	}
	json.Unmarshal(b2, &got)
	want := map[string]int{"dirt": 833, "smell": 30, "graffiti": 154, "incivism": 61}
	if fmt.Sprint(got.Raw) != fmt.Sprint(want) {
		t.Fatalf("n_raw = %v, want %v (json %s)", got.Raw, want, b2)
	}
	if got.Dirt != 91 {
		t.Fatalf("dirt percentile = %d, want 91", got.Dirt)
	}
}

// The shipped data/grid.json against the raw IRIS files, recounted here
// without loadIRIS or countIRIS: every hex's n_raw must equal the number of
// distinct complaint requests opened in the window at a point in that hex.
// Runs only when the IRIS files are in the download cache.
func TestShippedGridMatchesIRIS(t *testing.T) {
	paths, _ := filepath.Glob(".cache/iris_*.csv")
	if len(paths) == 0 {
		t.Skip("no IRIS files in .cache")
	}
	b, err := os.ReadFile("../data/grid.json")
	if err != nil {
		t.Skip("no data/grid.json")
	}
	var g Grid
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	win := g.Meta.Ranges["iris"]
	var fy, fm, ty, tm int
	fmt.Sscanf(win.From, "%d-%d", &fy, &fm)
	fmt.Sscanf(win.To, "%d-%d", &ty, &tm)
	from, to := fy*12+fm-1, ty*12+tm-1
	if to-from+1 != 24 {
		t.Errorf("window %s..%s is %d months, not 24", win.From, win.To, to-from+1)
	}

	type counts [nIris]int
	want := map[string]*counts{}
	seen := map[string]bool{}
	var copies, copiesCounted int
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		r := csv.NewReader(f)
		r.FieldsPerRecord, r.LazyQuotes = -1, true
		rows, err := r.ReadAll()
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		ci := map[string]int{}
		for i, h := range rows[0] {
			ci[strings.TrimPrefix(h, "\xEF\xBB\xBF")] = i
		}
		for _, r := range rows[1:] {
			typ := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(r[ci["TIPUS"]])), "È", "E")
			if typ != "INCIDENCIA" && typ != "QUEIXA" && typ != "ISSUE" && typ != "COMPLAINT" {
				continue
			}
			var y, m int
			fmt.Sscanf(r[ci["ANY_DATA_ALTA"]]+" "+r[ci["MES_DATA_ALTA"]], "%d %d", &y, &m)
			if mo := y*12 + m - 1; mo < from || mo > to {
				continue
			}
			cat := classify(r[ci["AREA"]], r[ci["ELEMENT"]], r[ci["DETALL"]])
			if cat == catNone {
				continue
			}
			var lat, lon float64
			if _, err := fmt.Sscanf(r[ci["LATITUD"]]+" "+r[ci["LONGITUD"]], "%g %g", &lat, &lon); err != nil {
				continue
			}
			c, err := h3.LatLngToCell(h3.NewLatLng(lat, lon), g.Meta.H3Resolution)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := g.Cells[c.String()]; !ok {
				continue
			}
			if id := r[ci["FITXA_ID"]]; seen[id] {
				copies++
				continue
			} else {
				seen[id] = true
			}
			if want[c.String()] == nil {
				want[c.String()] = &counts{}
			}
			want[c.String()][cat]++
			copiesCounted++
		}
	}

	var bad int
	var tot [nIris]int
	for k, cell := range g.Cells {
		w := counts{}
		if want[k] != nil {
			w = *want[k]
		}
		got := counts{cell.Raw.Dirt, cell.Raw.Smell, cell.Raw.Graffiti, cell.Raw.Incivism}
		for i := range tot {
			tot[i] += got[i]
		}
		if got != w {
			if bad < 10 {
				t.Errorf("hex %s: n_raw %v, source has %v (dirt, smell, graffiti, incivism)", k, got, w)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d hexes disagree with the source; %d verbatim duplicate rows fell in these hexes", bad, len(g.Cells), copies)
	}
	for i, name := range irisCatNames {
		if g.Meta.Totals[name] != tot[i] {
			t.Errorf("meta.totals.%s = %d, but the hexes add up to %d", name, g.Meta.Totals[name], tot[i])
		}
	}
	t.Logf("%d hexes, %d complaint requests recounted from %d files (window %s..%s)", len(g.Cells), copiesCounted, len(paths), win.From, win.To)
}

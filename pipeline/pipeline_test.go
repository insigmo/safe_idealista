package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

// The extension indexes with h3-js; both must agree with the H3 reference.
func TestH3MatchesReference(t *testing.T) {
	c, err := h3.LatLngToCell(h3.NewLatLng(37.3615593, -122.0553238), 7)
	if err != nil || c.String() != "87283472bffffff" {
		t.Fatalf("got %v %v, want 87283472bffffff", c, err)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		area, element, detail string
		want                  int
	}{
		{"Recollida i neteja de l'espai urbà", "Neteja carrers i/o places", "Orins  excrements humans", catSmell},
		{"Recollida i neteja de l'espai urbà", "Neteja de l'espai públic", "Orins, excrements animals", catSmell},
		{"Recollida i neteja de l'espai urbà", "Neteja de l'espai públic", "Objectes a netejar / retirar", catDirt},
		{"Recollida i neteja de l'espai urbà", "Pintades / cartells / pancartes", "Netejar pintada  cartell o pancarta en espai públic", catGraffiti},
		{"Recollida i neteja de l'espai urbà", "Recollida orgànic / rebuig", "Contenidor orgànic/rebuig cremat", catIncivism},
		{"Recollida i neteja de l'espai urbà", "Recollida paper cartró/vidre/reciclables", "Contenidors de paper i cartró/vidre/reciclables", catDirt},
		{"Prevenció i seguretat", "Molèsties soroll a la via pública", "Concentració persones fent soroll", catIncivism},
		{"Prevenció i seguretat", "Molèsties soroll a la via pública", "Obres a la via pública.", catNone},
		{"Prevenció i seguretat", "Incompliment d'ordenances a l'espai públic", "Drogues", catIncivism},
		{"Prevenció i seguretat", "Incompliment d'ordenances a l'espai públic", "Necessitats fisiològiques a la via pública", catSmell},
		{"Sanitat i salut pública", "Plagues urbanes", "Rates o ratolins en espais públics", catDirt},
		{"Manteniment de l'espai urbà", "Clavegueram", "Clavegueram pudor xarxa", catSmell},
		{"Manteniment de l'espai urbà", "Enllumenat públic", "Enllumenat incidències", catNone},
		{"Recollida i neteja de l'espai urbà", "Personal de neteja", "Comportament personal de neteja", catNone},
	}
	for _, c := range cases {
		if got := classify(c.area, c.element, c.detail); got != c.want {
			t.Errorf("%s | %s | %s: got %s, want %s", c.area, c.element, c.detail, catLabel(got), catLabel(c.want))
		}
	}
}

func TestShapeContainsWithHole(t *testing.T) {
	sh, err := parseWKT("POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0), (4 4, 6 4, 6 6, 4 6, 4 4))")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		lon, lat float64
		in       bool
	}{{1, 1, true}, {5, 5, false}, {11, 5, false}, {7, 7, true}} {
		if got := sh.Contains(c.lon, c.lat); got != c.in {
			t.Errorf("(%v,%v): got %v want %v", c.lon, c.lat, got, c.in)
		}
	}
	multi, err := parseWKT("MULTIPOLYGON (((0 0, 1 0, 1 1, 0 0)), ((5 5, 6 5, 6 6, 5 5)))")
	if err != nil || len(multi.Rings) != 2 {
		t.Fatalf("multipolygon: %v rings, err %v", len(multi.Rings), err)
	}
}

func TestPercentileOf(t *testing.T) {
	ref := []float64{1, 2, 2, 3}
	for _, c := range []struct{ v, want float64 }{{0, 0}, {1, 12.5}, {2, 50}, {3, 87.5}, {4, 100}} {
		if got := percentileOf(ref, c.v); got != c.want {
			t.Errorf("percentileOf(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestSpanishNumber(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
		ok   bool
	}{{"14.781,0", 14781, true}, {"152.469", 152469, true}, {"-13,0", -13, true}, {"", 0, false}, {".", 0, false}} {
		got, ok := spanishNumber(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("spanishNumber(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestWeightedPercentiles(t *testing.T) {
	// 1 has weight 3, 2 has weight 1: three quarters of the weight sits at 1.
	p := weightedPercentiles([]float64{2, 1}, []float64{1, 3})
	for _, c := range []struct{ v, want float64 }{{0, 0}, {1, 37.5}, {2, 87.5}, {3, 100}} {
		if got := p(c.v); got != c.want {
			t.Errorf("p(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestUTMCentralMeridian(t *testing.T) {
	p := utmToLonLat(500000, 0, -3)
	if math.Abs(p.Lon+3) > 1e-12 || math.Abs(p.Lat) > 1e-12 {
		t.Fatalf("origin: got %+v", p)
	}
	// Meridian arc of GRS80 at 40°N is 4,429,529.03 m; UTM scales it by 0.9996.
	p = utmToLonLat(500000, 0.9996*4429529.03, -3)
	if math.Abs(p.Lat-40) > 1e-6 || math.Abs(p.Lon+3) > 1e-12 {
		t.Fatalf("40N: got %+v", p)
	}
}

func TestRingWinding(t *testing.T) {
	cw := []pt{{0, 0}, {0, 1}, {1, 1}, {1, 0}, {0, 0}}
	if ringArea(cw) >= 0 {
		t.Fatalf("clockwise ring should have negative area, got %v", ringArea(cw))
	}
	cells, err := shapeCells(&Shape{Rings: [][]pt{{{2.15, 41.38}, {2.15, 41.40}, {2.17, 41.40}, {2.17, 41.38}, {2.15, 41.38}}}}, 9)
	if err != nil || len(cells) < 20 {
		t.Fatalf("polyfill of a 2 km square: %d cells, err %v", len(cells), err)
	}
}

// INE's section boundaries (UTM 30) and Barcelona's own (WGS84) describe the
// same sections, drawn independently. Once the projection is undone, a point
// inside each INE section must fall in the Barcelona section with the same
// code, and there must be no systematic shift between the two.
// Runs only when both files are in the download cache.
func TestINESectionsMatchBarcelona(t *testing.T) {
	zips, _ := filepath.Glob(".cache/ine_seccionado_*.zip")
	if len(zips) == 0 {
		t.Skip("no INE section file in .cache")
	}
	bcn, err := loadSections(".cache/seccions_censals.csv")
	if err != nil {
		t.Skip("no Barcelona section file in .cache")
	}
	recs, err := readZippedShapefile(zips[len(zips)-1], func(x, y float64) pt { return utmToLonLat(x, y, utmZone30CM) })
	if err != nil {
		t.Fatal(err)
	}
	var n, same int
	var dx, dy float64
	for _, r := range recs {
		code := r.Attrs["CUSEC"]
		if !strings.HasPrefix(code, "08019") {
			continue
		}
		s, ok := bcn[atoi(code[5:7])*1000+atoi(code[7:])]
		if !ok {
			continue
		}
		cells, err := shapeCells(r.Shape, 10)
		if err != nil || len(cells) == 0 {
			continue
		}
		ll, _ := cells[len(cells)/2].LatLng()
		n++
		if s.Shape.Contains(ll.Lng, ll.Lat) {
			same++
		}
		a, b := r.Shape, s.Shape
		dx += ((a.MinLon + a.MaxLon) - (b.MinLon + b.MaxLon)) / 2 * 84000 // metres at 41°N
		dy += ((a.MinLat + a.MaxLat) - (b.MinLat + b.MaxLat)) / 2 * 111000
	}
	if n < 1000 {
		t.Fatalf("only %d Barcelona sections compared", n)
	}
	dx, dy = dx/float64(n), dy/float64(n)
	share := float64(same) / float64(n)
	if share < 0.97 || math.Hypot(dx, dy) > 5 {
		t.Fatalf("%.1f%% of %d INE section points fall in the same Barcelona section; mean bbox shift (%.1f, %.1f) m", 100*share, n, dx, dy)
	}
	t.Logf("%.1f%% of %d INE section points fall in the same Barcelona section; mean bbox shift (%.1f, %.1f) m", 100*share, n, dx, dy)
}

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"Palmas de Gran Canaria, Las": "Las Palmas de Gran Canaria",
		"Hospitalet de Llobregat, L'": "L'Hospitalet de Llobregat",
		"Vila Joiosa, la/Villajoyosa": "La Vila Joiosa/Villajoyosa",
		"Pamplona/Iruña":              "Pamplona/Iruña",
		"Coruña, A":                   "A Coruña",
		"Madrid":                      "Madrid",
	} {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMadridClassify(t *testing.T) {
	cases := []struct {
		l1, l2 string
		want   int
	}{
		{"Limpieza en espacios públicos", "Limpieza de calles", catDirt},
		{"Limpieza y pintadas", "Limpieza en vías públicas", catDirt},
		{"Limpieza y pintadas", "Pintadas y grafitis", catGraffiti},
		{"Cubos y contenedores", "Vaciado de cubo o contenedor", catDirt},
		{"Cubos y contenedores", "Nuevo cubo o contenedor", catNone},
		{"Retirada de elementos", "Muebles abandonados en vía pública", catDirt},
		{"Retirada de elementos", "Muebles particulares", catNone},
		{"Ratas y cucarachas", "", catDirt},
		{"Retirada de elementos", "Animal muerto", catNone},
		{"Calzadas y aceras", "Desperfecto en acera", catNone},
		{"zz[Pruebas Betatester]", "Limpieza mobiliario urbano o áreas infantiles", catNone},
	}
	for _, c := range cases {
		if got := madridClassify(c.l1, c.l2); got != c.want {
			t.Errorf("%s | %s: got %s, want %s", c.l1, c.l2, catLabel(got), catLabel(c.want))
		}
	}
}

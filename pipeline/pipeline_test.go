package main

import (
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

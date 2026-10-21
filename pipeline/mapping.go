package main

import (
	"strings"
	"unicode"
)

// IRIS-derived categories. Crime comes from police sources, not IRIS.
const (
	catDirt = iota
	catSmell
	catGraffiti
	catIncivism
	nIris
	catNone = -1
)

var irisCatNames = [nIris]string{"dirt", "smell", "graffiti", "incivism"}

// rule matches normalised substrings of AREA, ELEMENT and DETALL ("" = any).
// Rules are evaluated in order and the first match wins, so the specific
// smell/incivism rules sit above the broad cleaning ones.
type rule struct {
	cat                   int
	area, element, detail string
}

const (
	areaNeteja    = "recollida i neteja"
	areaSeguretat = "prevencio i seguretat"
	elSoroll      = "molesties soroll a la via publica"
	elOrdPublic   = "incompliment d ordenances a l espai public"
	elOrdPrivat   = "incompliment d ordenances a l espai privat"
)

var rules = []rule{
	// smell: urine, excrement, odours
	{catSmell, "", "", "orins"},
	{catSmell, "", "", "excrements"},
	{catSmell, "", "", "males olors"},
	{catSmell, "", "", "pudor"},
	{catSmell, "", "", "necessitats fisiologiques"},

	// graffiti
	{catGraffiti, areaNeteja, "pintades", ""},
	{catGraffiti, "", "", "pintades i grafits"},

	// incivism: people-made street noise, drinking, drugs, vandalism, squatting
	{catIncivism, areaSeguretat, elSoroll, "concentracio persones"},
	{catIncivism, areaSeguretat, elSoroll, "festes"},
	{catIncivism, areaSeguretat, elSoroll, "sortida locals nocturns"},
	{catIncivism, areaSeguretat, elSoroll, "musics al carrer"},
	{catIncivism, areaSeguretat, elSoroll, "terrasses"},
	{catIncivism, areaSeguretat, elSoroll, "activitats comercials"},
	{catIncivism, areaSeguretat, "molesties espai public", "persones"},
	{catIncivism, areaSeguretat, elOrdPublic, "vandalisme"},
	{catIncivism, areaSeguretat, elOrdPublic, "drogues"},
	{catIncivism, areaSeguretat, elOrdPublic, "begudes alcoholiques"},
	{catIncivism, areaSeguretat, elOrdPublic, "comerc ambulant"},
	{catIncivism, areaSeguretat, elOrdPublic, "molesties gossos"},
	{catIncivism, areaSeguretat, elOrdPublic, "horaris locals"},
	{catIncivism, areaSeguretat, elOrdPrivat, "ocupacio habitatges"},
	{catIncivism, areaNeteja, "", "cremat"}, // burnt containers

	// dirt: street cleaning, overflowing/misused containers, dumped items, rats
	{catDirt, areaNeteja, "neteja de l espai public", ""},
	{catDirt, areaNeteja, "neteja carrers", ""},
	{catDirt, areaNeteja, "recollida organic", ""},
	{catDirt, areaNeteja, "recollida paper", ""},
	{catDirt, areaNeteja, "recollida de mobles", ""},
	{catDirt, areaNeteja, "recollida comercial", ""},
	{catDirt, areaNeteja, "recollida d objectes perillosos", ""},
	{catDirt, areaNeteja, "recollida pneumatica", ""},
	{catDirt, areaNeteja, "papereres", ""},
	{catDirt, areaNeteja, "neteja solar", ""},
	{catDirt, areaNeteja, "parcs", ""},
	{catDirt, areaNeteja, "jocs infantils", ""},
	{catDirt, areaNeteja, "bancs", ""},
	{catDirt, areaNeteja, "zona gossos", ""},
	{catDirt, areaNeteja, "jardinera", ""},
	{catDirt, areaNeteja, "escales al carrer", ""},
	{catDirt, areaNeteja, "sacs i contenidors runa", ""},
	{catDirt, areaNeteja, "platges neteja", ""},
	{catDirt, "", "", "rates o ratolins"},
}

var foldAccents = strings.NewReplacer(
	"à", "a", "á", "a", "è", "e", "é", "e", "í", "i", "ï", "i",
	"ò", "o", "ó", "o", "ú", "u", "ü", "u", "ç", "c", "ñ", "n",
)

// norm lower-cases, strips Catalan/Spanish accents and collapses punctuation
// to single spaces, so "Orins, excrements" and "Orins  excrements" compare equal.
func norm(s string) string {
	s = foldAccents.Replace(strings.ToLower(s))
	var b strings.Builder
	space := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func classify(area, element, detail string) int {
	a, e, d := norm(area), norm(element), norm(detail)
	for _, r := range rules {
		if strings.Contains(a, r.area) && strings.Contains(e, r.element) && strings.Contains(d, r.detail) {
			return r.cat
		}
	}
	return catNone
}

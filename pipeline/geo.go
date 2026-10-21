package main

import (
	"fmt"
	"strconv"
	"strings"
)

type pt struct{ Lon, Lat float64 }

// Shape is a polygon or multipolygon flattened to its rings. Point-in-polygon
// uses the even-odd rule over all rings, which handles holes and disjoint parts.
type Shape struct {
	Rings                          [][]pt
	MinLon, MinLat, MaxLon, MaxLat float64
}

// parseWKT reads POLYGON/MULTIPOLYGON WKT in lon/lat order.
func parseWKT(s string) (*Shape, error) {
	open := strings.IndexByte(s, '(')
	if open < 0 {
		return nil, fmt.Errorf("wkt: no coordinates")
	}
	sh := &Shape{MinLon: 1e9, MinLat: 1e9, MaxLon: -1e9, MaxLat: -1e9}
	depth, start := 0, -1
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
			start = i + 1
		case ')':
			if start >= 0 {
				ring, err := parseRing(s[start:i])
				if err != nil {
					return nil, err
				}
				for _, p := range ring {
					sh.MinLon, sh.MaxLon = min(sh.MinLon, p.Lon), max(sh.MaxLon, p.Lon)
					sh.MinLat, sh.MaxLat = min(sh.MinLat, p.Lat), max(sh.MaxLat, p.Lat)
				}
				sh.Rings = append(sh.Rings, ring)
				start = -1
			}
			depth--
		}
	}
	if len(sh.Rings) == 0 || depth != 0 {
		return nil, fmt.Errorf("wkt: malformed")
	}
	return sh, nil
}

func parseRing(s string) ([]pt, error) {
	parts := strings.Split(s, ",")
	ring := make([]pt, 0, len(parts))
	for _, p := range parts {
		f := strings.Fields(p)
		if len(f) < 2 {
			return nil, fmt.Errorf("wkt: bad coordinate %q", p)
		}
		lon, err1 := strconv.ParseFloat(f[0], 64)
		lat, err2 := strconv.ParseFloat(f[1], 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("wkt: bad coordinate %q", p)
		}
		ring = append(ring, pt{lon, lat})
	}
	return ring, nil
}

func (sh *Shape) Contains(lon, lat float64) bool {
	if lon < sh.MinLon || lon > sh.MaxLon || lat < sh.MinLat || lat > sh.MaxLat {
		return false
	}
	in := false
	for _, r := range sh.Rings {
		for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
			a, b := r[i], r[j]
			if (a.Lat > lat) != (b.Lat > lat) && lon < (b.Lon-a.Lon)*(lat-a.Lat)/(b.Lat-a.Lat)+a.Lon {
				in = !in
			}
		}
	}
	return in
}

// Centroid is the vertex average of the outer ring; only used as a fallback
// location for census sections too small to contain a sample point.
func (sh *Shape) Centroid() pt {
	var c pt
	r := sh.Rings[0]
	for _, p := range r {
		c.Lon += p.Lon
		c.Lat += p.Lat
	}
	c.Lon /= float64(len(r))
	c.Lat /= float64(len(r))
	return c
}

// Rough city bounding box, used to drop mis-geocoded records.
func inBarcelona(lat, lon float64) bool {
	return lat > 41.31 && lat < 41.47 && lon > 2.05 && lon < 2.24
}

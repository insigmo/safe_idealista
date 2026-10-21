package main

import "math"

// utmToLonLat inverts a UTM projection on the GRS80 ellipsoid (ETRS89), using
// Krüger's series to third order. INE publishes the whole country, the
// Canaries included, in zone 30 (central meridian 3° W); the Canaries sit
// about 15° off that meridian, where the series is still good to well under a
// metre.
func utmToLonLat(easting, northing, centralMeridian float64) pt {
	const (
		a  = 6378137.0
		f  = 1 / 298.257222101
		k0 = 0.9996
		e0 = 500000.0
	)
	n := f / (2 - f)
	n2, n3 := n*n, n*n*n
	A := a / (1 + n) * (1 + n2/4 + n2*n2/64)
	beta := [3]float64{n/2 - 2*n2/3 + 37*n3/96, n2/48 + n3/15, 17 * n3 / 480}
	delta := [3]float64{2*n - 2*n2/3 - 2*n3, 7*n2/3 - 8*n3/5, 56 * n3 / 15}

	xi := northing / (k0 * A)
	eta := (easting - e0) / (k0 * A)
	xi1, eta1 := xi, eta
	for j := 1; j <= 3; j++ {
		k := 2 * float64(j)
		xi1 -= beta[j-1] * math.Sin(k*xi) * math.Cosh(k*eta)
		eta1 -= beta[j-1] * math.Cos(k*xi) * math.Sinh(k*eta)
	}
	chi := math.Asin(math.Sin(xi1) / math.Cosh(eta1))
	phi := chi
	for j := 1; j <= 3; j++ {
		phi += delta[j-1] * math.Sin(2*float64(j)*chi)
	}
	lambda := math.Atan2(math.Sinh(eta1), math.Cos(xi1))
	return pt{Lon: centralMeridian + lambda*180/math.Pi, Lat: phi * 180 / math.Pi}
}

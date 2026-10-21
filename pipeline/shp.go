package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"unicode/utf8"
)

// shpRecord is one polygon feature of an ESRI shapefile: its rings already
// converted to lon/lat, plus its dBASE attributes (trimmed text).
type shpRecord struct {
	Shape *Shape
	Attrs map[string]string
}

// readZippedShapefile reads the first .shp/.dbf pair inside a zip archive.
// Coordinates are converted with toLonLat; only polygon features are kept.
func readZippedShapefile(zipPath string, toLonLat func(x, y float64) pt) ([]shpRecord, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var shpData, dbfData, cpgData []byte
	for _, f := range zr.File {
		ext := strings.ToLower(path.Ext(f.Name))
		if ext != ".shp" && ext != ".dbf" && ext != ".cpg" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if ext == ".shp" && shpData == nil {
			shpData = b
		} else if ext == ".dbf" && dbfData == nil {
			dbfData = b
		} else if ext == ".cpg" && cpgData == nil {
			cpgData = b
		}
	}
	if shpData == nil || dbfData == nil {
		return nil, fmt.Errorf("%s: no .shp/.dbf pair", zipPath)
	}
	shapes, err := parseShp(shpData, toLonLat)
	if err != nil {
		return nil, err
	}
	isUTF8 := strings.EqualFold(strings.TrimSpace(string(cpgData)), "UTF-8")
	attrs, err := parseDbf(dbfData, isUTF8)
	if err != nil {
		return nil, err
	}
	if len(shapes) != len(attrs) {
		return nil, fmt.Errorf("%s: %d shapes but %d attribute rows", zipPath, len(shapes), len(attrs))
	}
	out := make([]shpRecord, 0, len(shapes))
	for i, sh := range shapes {
		if sh != nil {
			out = append(out, shpRecord{sh, attrs[i]})
		}
	}
	return out, nil
}

// parseShp decodes polygon records (type 5); null shapes come back as nil so
// that indexes still line up with the .dbf rows.
func parseShp(b []byte, toLonLat func(x, y float64) pt) ([]*Shape, error) {
	if len(b) < 100 || binary.BigEndian.Uint32(b) != 9994 {
		return nil, fmt.Errorf("shp: bad header")
	}
	var out []*Shape
	for off := 100; off+8 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[off+4:])) * 2
		rec := b[off+8 : min(off+8+n, len(b))]
		off += 8 + n
		if len(rec) < 4 {
			return nil, fmt.Errorf("shp: truncated record")
		}
		switch t := binary.LittleEndian.Uint32(rec); t {
		case 0:
			out = append(out, nil)
			continue
		case 5:
		default:
			return nil, fmt.Errorf("shp: shape type %d not supported", t)
		}
		if len(rec) < 44 {
			return nil, fmt.Errorf("shp: truncated polygon")
		}
		nParts := int(binary.LittleEndian.Uint32(rec[36:]))
		nPoints := int(binary.LittleEndian.Uint32(rec[40:]))
		ptsAt := 44 + 4*nParts
		if len(rec) < ptsAt+16*nPoints {
			return nil, fmt.Errorf("shp: truncated polygon points")
		}
		sh := &Shape{MinLon: 1e9, MinLat: 1e9, MaxLon: -1e9, MaxLat: -1e9}
		for p := 0; p < nParts; p++ {
			from := int(binary.LittleEndian.Uint32(rec[44+4*p:]))
			to := nPoints
			if p+1 < nParts {
				to = int(binary.LittleEndian.Uint32(rec[44+4*(p+1):]))
			}
			ring := make([]pt, 0, to-from)
			for i := from; i < to; i++ {
				x := math.Float64frombits(binary.LittleEndian.Uint64(rec[ptsAt+16*i:]))
				y := math.Float64frombits(binary.LittleEndian.Uint64(rec[ptsAt+16*i+8:]))
				q := toLonLat(x, y)
				sh.MinLon, sh.MaxLon = min(sh.MinLon, q.Lon), max(sh.MaxLon, q.Lon)
				sh.MinLat, sh.MaxLat = min(sh.MinLat, q.Lat), max(sh.MaxLat, q.Lat)
				ring = append(ring, q)
			}
			if len(ring) >= 4 {
				sh.Rings = append(sh.Rings, ring)
			}
		}
		if len(sh.Rings) == 0 {
			out = append(out, nil)
			continue
		}
		out = append(out, sh)
	}
	return out, nil
}

// parseDbf decodes dBASE III rows, as UTF-8 when the .cpg file says so and
// as Latin-1 otherwise (enough for INE's ASCII code fields).
func parseDbf(b []byte, isUTF8 bool) ([]map[string]string, error) {
	if len(b) < 32 {
		return nil, fmt.Errorf("dbf: bad header")
	}
	nRec := int(binary.LittleEndian.Uint32(b[4:]))
	hdrLen := int(binary.LittleEndian.Uint16(b[8:]))
	recLen := int(binary.LittleEndian.Uint16(b[10:]))
	type field struct {
		name   string
		offset int
		width  int
	}
	var fields []field
	offset := 1 // deletion flag
	for p := 32; p+32 <= hdrLen && b[p] != 0x0d; p += 32 {
		name := string(bytes.TrimRight(b[p:p+11], "\x00"))
		w := int(b[p+16])
		fields = append(fields, field{name, offset, w})
		offset += w
	}
	if hdrLen+nRec*recLen > len(b) {
		return nil, fmt.Errorf("dbf: truncated")
	}
	out := make([]map[string]string, nRec)
	for i := range nRec {
		rec := b[hdrLen+i*recLen : hdrLen+(i+1)*recLen]
		m := make(map[string]string, len(fields))
		for _, f := range fields {
			raw := rec[f.offset : f.offset+f.width]
			if isUTF8 && utf8.Valid(raw) {
				m[f.name] = strings.TrimSpace(string(raw))
				continue
			}
			r := make([]rune, len(raw))
			for j, c := range raw {
				r[j] = rune(c)
			}
			m[f.name] = strings.TrimSpace(string(r))
		}
		out[i] = m
	}
	return out, nil
}

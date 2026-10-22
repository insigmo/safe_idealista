package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	h3 "github.com/uber/h3-go/v4/x/h3go"
)

type mapRow struct {
	Area, Element, Detail string
	Cat                   int
	N                     int
}

func catLabel(c int) string {
	if c == catNone {
		return "—"
	}
	return irisCatNames[c]
}

func writeMappingReport(path string, mapping map[[3]string]*mapRow, from, to month) error {
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
		return rows[i].Area+rows[i].Element+rows[i].Detail < rows[j].Area+rows[j].Element+rows[j].Detail
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# IRIS → category mapping (%s … %s)\n\n", from, to)
	fmt.Fprintf(&b, "Geocoded incidents/complaints in the window. Rules: `pipeline/mapping.go`.\n\n")
	fmt.Fprintf(&b, "| category | records |\n|---|---:|\n")
	for _, c := range []int{catDirt, catSmell, catGraffiti, catIncivism, catNone} {
		fmt.Fprintf(&b, "| %s | %d |\n", catLabel(c), tot[c])
		fmt.Printf("  %-9s %7d\n", catLabel(c), tot[c])
	}
	for _, c := range []int{catDirt, catSmell, catGraffiti, catIncivism} {
		fmt.Fprintf(&b, "\n## %s\n\n| records | AREA | ELEMENT | DETALL |\n|---:|---|---|---|\n", catLabel(c))
		for _, r := range rows {
			if r.Cat == c {
				fmt.Fprintf(&b, "| %d | %s | %s | %s |\n", r.N, r.Area, r.Element, r.Detail)
			}
		}
	}
	fmt.Fprintf(&b, "\n## Not mapped (≥ 150 records)\n\n| records | AREA | ELEMENT | DETALL |\n|---:|---|---|---|\n")
	for _, r := range rows {
		if r.Cat == catNone && r.N >= 150 {
			fmt.Fprintf(&b, "| %d | %s | %s | %s |\n", r.N, r.Area, r.Element, r.Detail)
		}
	}
	logf("wrote %s", path)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeRankings(path string, g Grid, cr *crimeResult) error {
	type br struct {
		code string
		BarriOut
	}
	var all []br
	for _, k := range sortedKeys(g.Barris) {
		all = append(all, br{k, g.Barris[k]})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })

	var b strings.Builder
	w := io.MultiWriter(&b, os.Stdout)
	table := func(title string, rows []br, startRank int) {
		fmt.Fprintf(w, "\n## %s\n\n| # | barri | district | residents | score | dirt | smell | graffiti | incivism | crime | crimes/1k/yr | income €/p |\n|---:|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n", title)
		for i, r := range rows {
			fmt.Fprintf(w, "| %d | %s | %s | %d | **%d** | %d | %d | %d | %d | %d | %.1f | %d |\n",
				startRank+i, r.Name, r.District, r.Pop, r.Score, r.Dirt, r.Smell, r.Graffiti, r.Incivism, r.Crime, r.CrimePer1k, r.Income)
		}
	}
	fmt.Fprintf(w, "# Barri rankings (default weights; percentiles 0 = best, 100 = worst)\n")
	n := min(20, len(all))
	table("Top 20 (best score)", all[:n], 1)
	table("Bottom 20 (worst score)", all[len(all)-n:], len(all)-n+1)

	dist := map[string][2]float64{} // score*pop, pop
	for _, r := range all {
		d := dist[r.District]
		dist[r.District] = [2]float64{d[0] + float64(r.Score*r.Pop), d[1] + float64(r.Pop)}
	}
	fmt.Fprintf(w, "\n## Districts (population-weighted barri score)\n\n| district | score |\n|---|---:|\n")
	dnames := sortedKeys(dist)
	sort.SliceStable(dnames, func(i, j int) bool {
		return dist[dnames[i]][0]/dist[dnames[i]][1] > dist[dnames[j]][0]/dist[dnames[j]][1]
	})
	for _, d := range dnames {
		fmt.Fprintf(w, "| %s | %.0f |\n", d, dist[d][0]/dist[d][1])
	}

	fmt.Fprintf(w, "\n## Crime types counted (%s … %s, Barcelona districts)\n\n| type | count |\n|---|---:|\n", cr.From, cr.To)
	types := sortedKeys(cr.ByType)
	sort.SliceStable(types, func(i, j int) bool { return cr.ByType[types[i]] > cr.ByType[types[j]] })
	for _, t := range types {
		fmt.Fprintf(w, "| %s | %.0f |\n", t, cr.ByType[t])
	}
	logf("wrote %s", path)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

type feature struct {
	Type     string         `json:"type"`
	Geometry map[string]any `json:"geometry"`
	Props    map[string]any `json:"properties"`
}

func writeReviewMap(path string, g Grid, hexes map[h3.Cell]*Hex) error {
	var feats []feature
	for _, id := range sortedKeys(g.Cells) {
		c := g.Cells[id]
		bnd, err := h3.CellToBoundary(h3.CellFromString(id))
		if err != nil {
			return err
		}
		ring := make([][2]float64, 0, len(bnd)+1)
		for _, v := range bnd {
			ring = append(ring, [2]float64{round6(v.Lng), round6(v.Lat)})
		}
		ring = append(ring, ring[0])
		b := g.Barris[strconv.Itoa(c.Barri)]
		feats = append(feats, feature{
			Type:     "Feature",
			Geometry: map[string]any{"type": "Polygon", "coordinates": [][][2]float64{ring}},
			Props: map[string]any{
				"id": id, "score": c.Score, "dirt": c.Dirt, "smell": c.Smell, "graffiti": c.Graffiti,
				"incivism": c.Incivism, "crime": c.Crime, "pop": c.Pop, "inc": c.Income, "lp": c.LowPop,
				"raw": c.Raw, "barri": b.Name, "district": b.District,
			},
		})
	}
	data, err := json.Marshal(map[string]any{
		"type": "FeatureCollection", "features": feats,
		"meta": map[string]any{"ranges": g.Meta.Ranges, "generated_at": g.Meta.GeneratedAt, "caveats": g.Meta.Caveats},
	})
	if err != nil {
		return err
	}
	html := strings.Replace(reviewMapHTML, "/*DATA*/", string(data), 1)
	logf("wrote %s (%d KB)", path, len(html)/1024)
	return os.WriteFile(path, []byte(html), 0o644)
}

func round6(v float64) float64 { return float64(round(v*1e6)) / 1e6 }

const reviewMapHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Safe Idealista — review map</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css">
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
<style>
  html, body, #map { height: 100%; margin: 0; }
  body { font: 13px/1.4 system-ui, sans-serif; }
  .panel { position: absolute; top: 10px; right: 10px; z-index: 1000; background: #fff; padding: 10px 12px;
           border-radius: 6px; box-shadow: 0 1px 4px rgba(0,0,0,.3); max-width: 300px; }
  .panel h1 { font-size: 14px; margin: 0 0 6px; }
  .panel label { display: block; margin: 4px 0; }
  .legend { display: flex; height: 10px; margin: 6px 0 2px; }
  .legend span { flex: 1; }
  .small { color: #555; font-size: 11px; }
</style>
</head>
<body>
<div id="map"></div>
<div class="panel">
  <h1>Safe Idealista — review</h1>
  <label>Layer
    <select id="layer">
      <option value="score">Score (100 = best)</option>
      <option value="dirt">Dirt percentile</option>
      <option value="smell">Smell percentile</option>
      <option value="graffiti">Graffiti percentile</option>
      <option value="incivism">Incivism percentile</option>
      <option value="crime">Crime percentile (district-based estimate)</option>
      <option value="inc">Income €/person</option>
    </select>
  </label>
  <label>Opacity <input id="opacity" type="range" min="0" max="1" step="0.05" value="0.6"></label>
  <label><input id="hidelp" type="checkbox"> Hide cells with &lt;100 residents</label>
  <div class="legend" id="legend"></div>
  <div class="small" id="legendtext"></div>
  <p class="small" id="ranges"></p>
</div>
<script id="data" type="application/json">/*DATA*/</script>
<script>
const data = JSON.parse(document.getElementById('data').textContent);
const map = L.map('map').setView([41.395, 2.165], 13);
L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
  maxZoom: 19, attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);

// green → yellow → red for "badness" 0..100
function ramp(t) {
  t = Math.max(0, Math.min(1, t));
  const h = 120 * (1 - t);
  return 'hsl(' + h + ',75%,45%)';
}
let incMin = Infinity, incMax = -Infinity;
for (const f of data.features) if (f.properties.inc > 0) { incMin = Math.min(incMin, f.properties.inc); incMax = Math.max(incMax, f.properties.inc); }
function badness(p, layer) {
  if (layer === 'score') return 1 - p.score / 100;
  if (layer === 'inc') return p.inc > 0 ? 1 - (p.inc - incMin) / (incMax - incMin) : null;
  return p[layer] / 100;
}
const $ = id => document.getElementById(id);
let geo;
function draw() {
  const layer = $('layer').value, op = +$('opacity').value, hide = $('hidelp').checked;
  if (geo) geo.remove();
  geo = L.geoJSON(data, {
    filter: f => !(hide && f.properties.lp),
    style: f => {
      const b = badness(f.properties, layer);
      return { weight: 0.3, color: '#333', fillColor: b == null ? '#999' : ramp(b), fillOpacity: op };
    },
    onEachFeature: (f, l) => {
      const p = f.properties;
      l.bindTooltip('<b>' + p.barri + '</b> (' + p.district + ')<br>score <b>' + p.score + '</b> · residents ' + p.pop + (p.lp ? ' (low)' : '') +
        '<br>dirt ' + p.dirt + ' · smell ' + p.smell + ' · graffiti ' + p.graffiti + ' · incivism ' + p.incivism + ' · crime ' + p.crime +
        '<br>raw complaints: dirt ' + p.raw.dirt + ', smell ' + p.raw.smell + ', graffiti ' + p.raw.graffiti + ', incivism ' + p.raw.incivism +
        (p.inc ? '<br>income ' + p.inc + ' €/person' : '') + '<br><span class="small">' + p.id + '</span>', { sticky: true });
    }
  }).addTo(map);
  $('legend').innerHTML = Array.from({ length: 10 }, (_, i) => '<span style="background:' + ramp(i / 9) + '"></span>').join('');
  $('legendtext').textContent = layer === 'score' ? 'best ← → worst' : layer === 'inc' ? 'high income ← → low income' : 'low percentile (good) ← → high (bad)';
}
const r = data.meta.ranges;
$('ranges').innerHTML = 'Complaints ' + r.iris.from + ' … ' + r.iris.to + '<br>Crime ' + r.crime.from + ' … ' + r.crime.to +
  '<br>Generated ' + data.meta.generated_at + '<br><br>' + data.meta.caveats.join('<br>');
['layer', 'opacity', 'hidelp'].forEach(id => $(id).addEventListener('input', draw));
draw();
</script>
</body>
</html>
`

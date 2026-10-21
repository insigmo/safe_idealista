# BCN Street Quality — working notes

Status: Part 1 (data pipeline) is reviewed. Part 2 (extension) is built and tested as described under "How the extension was tested". Installing the packaged extension in a real Chrome is still to do.

## Datasets used

Each one was found through the portal's search API and checked by downloading a sample before any code was written. The pipeline resolves resource URLs from the dataset ID at run time, so new yearly files are picked up automatically.

| Layer | Portal | Dataset ID | Resource(s) | Fields used | Granularity | Window in current grid |
|---|---|---|---|---|---|---|
| Complaints | Open Data BCN (CKAN) | `iris` | `{2024,2025,2026}_IRIS_Peticions_Ciutadanes_OpenData.csv` | `TIPUS, AREA, ELEMENT, DETALL, ANY/MES_DATA_ALTA, LATITUD, LONGITUD` | point (address) | 2024-04 … 2026-03 (the 2026 file ends in March) |
| Crime | Dades obertes de Catalunya (Socrata) | `qnyt-emjc` — *Fets penals coneguts, fets coneguts resolts i detencions* | SoQL aggregate, `regi_policial_rp='RP Metropolitana Barcelona'` | `rea_b_sica_policial_abp, tipus_de_fet, any, mes, coneguts` | **district** (10 ABPs = 10 districts), monthly | 2024-10 … 2026-09 |
| Crime split within districts | Open Data BCN | `incidents-gestionats-gub` | `{2024,2025}_incidents_gestionats_gub.csv` | `Codi_Incident, Codi_barri, Numero_incidents_GUB` | barri, monthly | 2024-01 … 2025-12 |
| Residents | Open Data BCN | `pad_mdbas_sexe` | `2026_pad_mdbas_sexe.csv` | `Seccio_Censal, Valor` | census section (1,068) | 2026-01-01 |
| Geometry | Open Data BCN | `20170706-districtes-barris` | `BarcelonaCiutat_SeccionsCensals.csv`, `BarcelonaCiutat_Barris.csv` | codes, `geometria_wgs84` (WKT, lon lat) | section / barri polygons | current |
| Income (context only) | Open Data BCN | `renda-disponible-llars-bcn` | `2023_renda_disponible_llars_per_persona.csv` | `Codi_Districte, Seccio_Censal, Import_Euros` | census section | 2023 |

Notes on the files:
- IRIS: valid UTF‑8. Part of the 2025 file has its `TIPUS` values in English (`ISSUE`, `COMPLAINT`, …), but its `AREA/ELEMENT/DETALL` values are still in Catalan. About 1/3 of the rows have no coordinates, mostly queries (`CONSULTA`) about procedures.
- GUB incidents: some text fields are mis-encoded (mixed encodings), so the pipeline uses only the numeric codes. Rows with an empty barri are city-wide and are skipped.
- Mossos: `ABP Barcelona` is a small catch-all with no district, so it is ignored. The column names are Socrata's generated ones (`rea_b_sica_policial_abp`, `t_tol_codi_penal`); they are taken from the API response.

### Considered, not used

| Dataset | Why not |
|---|---|
| `punts-critics-neteja-barcelona` (critical cleaning spots) | It only covers **2017–2018** (last modified 2022). Eight-year-old hotspots would mislead, so it is left out of the score. It could be added as an optional historical layer. |
| `espais-prioritaris-neteja-barcelona` | 2019 only. Same reason. |
| Enquesta de victimització | A CKAN search for `victimitzacio` on Open Data BCN finds nothing. The survey is published as district reports, so it adds nothing finer than the Mossos data. I did not look for it outside CKAN. |
| Mossos hate-crime / traffic / domestic-violence datasets | Too specific, or no finer granularity. |

### Crime granularity (what actually exists)

The most granular official crime data is **Mossos by ABP**. In Barcelona each ABP is exactly one district, so there are **10 areas**. Open Data BCN has no geolocated crime data. Guàrdia Urbana publishes accidents and "incidents gestionats" **per barri**, but those are police call-outs (fights, street nuisance, street vending, traffic…), not recorded crimes.

How the crime layer is built:
1. District crime count = Mossos "fets coneguts" for street-relevant types over the last 24 months: theft, robbery (with violence or force), vehicle theft, criminal damage, assault/injury, threats, sexual assault, drug offences, homicide, squatting, other public-order offences. Fraud, forgery, traffic and justice-administration offences are left out.
2. Each district's count is split over its barris with weight `0.5 × GUB share + 0.5 × population share`. The GUB proxy uses codes 410 (*activitats molestes en espais públics*), 411 (*venda ambulant*) and 510 (*baralles*). The 0.5 is a flag: `-crime-gub-alpha`.
3. The barri rate is crimes per resident, shrunk toward the city rate (prior = 2,000 residents). Each hex takes its barri's rate.

**So the crime layer is a district figure split into barris by an estimate. It is not hex-level crime.** The popup and the map both say so.

## Category mapping

The rules live in [`pipeline/mapping.go`](pipeline/mapping.go). They match accent- and punctuation-insensitive substrings of `AREA | ELEMENT | DETALL`, first match wins. Only `INCIDÈNCIA/QUEIXA` records count (plus their English labels `ISSUE/COMPLAINT`). Queries, suggestions, service requests and thanks are left out.

| Category | What maps to it | Records (24 mo) |
|---|---|---:|
| `dirt` | *Recollida i neteja*: street/public-space cleaning, organic/refuse and recycling containers (overflowing, not emptied, displaced, misused), dumped furniture, commercial waste, dangerous objects (syringes…), pneumatic collection, full/dirty litter bins, dirty parks / playgrounds / benches / dog areas / planters / street stairs, rubble bags, beach cleaning, dirty municipal plots; plus *rates o ratolins* (rats) | 131,275 |
| `smell` | any DETALL with *orins* / *excrements* (urine, excrement, human or animal), *males olors*, *clavegueram pudor*, *necessitats fisiològiques a la via pública* | 6,188 |
| `graffiti` | *Pintades / cartells / pancartes*; *neteja de pintades i grafits* on monuments | 23,460 |
| `incivism` | street noise from people (*concentració persones*, *festes*, *sortida locals nocturns*, *músics*, *terrasses*, *activitats comercials*); *persones/col·lectius molestant*; *vandalisme*, *drogues*, *consum de begudes alcohòliques*, *comerç ambulant*, *molèsties gossos*, *incompliment horaris locals*; *ocupació habitatges*; burnt containers | 10,001 |
| — (not mapped) | pavement, trees, street lights, traffic lights, roadworks, bikes and vehicles, pests other than rats, pigeons, *convivència veïnal* (disputes between neighbours inside buildings), noise from works/traffic/AC/cleaning services, cleaning-staff behaviour | 175,543 |

The full table with counts for every `AREA | ELEMENT | DETALL` combination is in [`data/mapping.md`](data/mapping.md).

Choices you may want to change: *convivència veïnal* (2,388 records) is left out because it is about a single building, not the street. Abandoned bikes and vehicles, dead animals, pigeons and insects are also left out. Rats count as dirt.

## Method

1. **Residents per hex.** Each census section's padró population is spread evenly over a 20 m lattice of points inside its polygon (area-weighted dasymetric allocation) and summed per H3 res-9 cell. This gives 1,157 cells covering the municipality, with 1,729,963 residents. A cell's barri is the one most of its residents live in. Barri aggregates credit every barri in a cell by its residents there.
2. **Counts.** Every mapped IRIS record in the window goes to its H3 cell. None fell outside the city cells.
3. **Rates.** For each cell, `(count + ½·Σ neighbour counts + 1000·city rate) / (residents + ½·Σ neighbour residents + 1000)`. This combines first-ring smoothing with shrinkage toward the city rate, so cells with few residents are not dominated by noise.
4. **Percentiles.** Rates are ranked city-wide (mid-rank) against the cells with ≥100 residents. 0 = cleanest/safest, 100 = worst. Cells under 100 residents (443 of them: Collserola, Montjuïc, port, Zona Franca, beaches) are flagged `lp`.
5. **Score.** `100 − Σ wᵢ·percentileᵢ / Σ wᵢ`, default weights crime 0.35, smell 0.2, dirt 0.2, graffiti 0.15, incivism 0.1. Per-category percentiles are stored, so the extension can re-weight.

H3 comes from `github.com/uber/h3-go/v4/x/h3go`, Uber's pure-Go port (pinned pseudo-version from master; the cgo bindings would need a C compiler). A unit test checks it against the H3 reference value.

## Output — `data/grid.json` (233 KB uncompressed)

```jsonc
{
  "meta": { "generated_at", "h3_resolution": 9, "categories", "default_weights",
            "date_ranges": { "iris", "crime", "gub_incidents", "population", "income" },
            "sources": [...], "method": [...], "caveats": [...], "totals": {...} },
  "cells": { "89394460003ffff": { "dirt": 2, "smell": 14, "graffiti": 24, "incivism": 4, "crime": 15,
             "score": 88, "pop": 4956, "barri": 25, "inc": 38345, "lp": false,
             "n_raw": { "dirt": 121, "smell": 10, "graffiti": 29, "incivism": 16 } } },
  "barris": { "1": { "name": "el Raval", "district": "Ciutat Vella", "pop": 50863,
             "dirt": 93, ..., "score": 12, "inc": 17144, "crime_est": 25694,
             "crime_per_1k_year": 252.6, "n_raw": {...} } }
}
```
`n_raw.crime` is not stored per cell, because crime is not observed at cell level. Barri entries carry `crime_est` instead. Barri entries are what "approx" badges use when a listing has no coordinates.

## Sanity check (current grid)

Full tables are in [`data/rankings.md`](data/rankings.md).
- **Bottom 4 = all of Ciutat Vella** (Sant Pere/Santa Caterina/Ribera 4, Barri Gòtic 6, Barceloneta 12, Raval 12). Below them come Vila Olímpica, Parc i Llacuna, Poblenou, Sant Antoni and Dreta de l'Eixample.
- **Top 2 = Sarrià–Sant Gervasi** (Tres Torres 83, Bonanova 82). Galvany is 8th and Sarrià 11th. By district, Sarrià–Sant Gervasi ranks first (74) and Ciutat Vella last (8).
- **Notable: Nou Barris ranks second by district (72).** Roquetes, Porta, Prosperitat, Trinitat Nova and Ciutat Meridiana are all in the top 10. Mossos crime per resident there really is low (≈30–40 per 1k per year vs 250–320 in Ciutat Vella). But its complaint percentiles are low partly because **lower-income areas file fewer IRIS complaints**. This is the reporting-bias caveat showing up in the data.

## Idealista integration findings

Inspected 2026-10-21 in a desktop-width browser (1440×900), on `/alquiler-viviendas/barcelona-barcelona/` and its map view. All selectors live in [`extension/src/selectors.ts`](extension/src/selectors.ts).

### List view (`/alquiler-viviendas/barcelona-barcelona/`, `…/pagina-N.htm`)
- Server-rendered. Pagination does full page loads (`pagina-2.htm`); there is no infinite scroll. A MutationObserver still watches for cards that get re-rendered.
- Card: `article.item[data-element-id]`. `data-element-id` is the listing ID. Ads are `article.adv` and have no ID.
- Title link: `a.item-link` (its `title` attribute is e.g. `Piso en Calle de Mallorca, L'Antiga Esquerra de l'Eixample, Barcelona`). The text sits in `.item-info-container`, and the price is in `.price-row`.
- **Coordinates:** there are no `lat`/`lng` attributes in the markup. However, an inline `<script>` sets `var config = {…, listingMultimediaCarrousels: {"<id>": {…, "map": {"src": "https://maps.googleapis.com/maps/api/staticmap?…&center=41.37568530%2C2.16067930&…", "addressVisibility": "HIDDEN" | "EXACT"}}}}`. That's the "Mapa de la zona" slide of each card's carousel. The extension reads the `center=` parameter from the script **text** in the DOM. It does not need page globals and makes no requests. On the sample page **30 of 30** listings had it.
- `addressVisibility: HIDDEN` means the advertiser hid the exact address, so the point is approximate. Those badges are marked `≈`.
- Fallback when a card has no map entry: take the barri from the title (`…, <barri>, Barcelona`), match it to the grid's barri names (accent- and case-insensitive, plus a small alias table), use the barri average and mark the badge `approx`.

### Map view (`/alquiler-viviendas/barcelona-barcelona/mapa-google`)
- Note that `/mapa` (without `-google`) is a different page: a static district-picker image ("greymaps"), not the listings map.
- The listings map is **Google Maps JS API 3.65 ("quarterly")**. It is loaded by Idealista's React component through `@vis.gl/react-google-maps` (`solution_channel=GMP_visgl_rgmlibrary`, `callback=google.maps.__ib__`), uses raster rendering with a cloud `map_id`, and has 0 tilt and 0 heading. The container is `.google-map-container` inside `.multizone-map-container`.
- No global holds the map instance. **Hook:** in the page's main world, wrap a method on `google.maps.Map.prototype` that the instance calls constantly (`get`, which comes from MVCObject; it was called about 270 times while the map loaded). The first call with `this instanceof google.maps.Map` hands over the instance. Verified live: zoom 12, bounds OK.
- **Overlay:** a `google.maps.OverlayView` subclass draws one `<canvas>` in the `overlayLayer` pane, projecting hex vertices with `fromLatLngToDivPixel`. Google calls `draw()` on every pan and zoom, so the overlay stays in sync with no extra listeners. Verified live with a test dot on the old town.
- When the viewport changes, the page fetches `/ajax/listingcontroller/listingmapajaxnopins.ajax?…&zoom=12&northEast=…&southWest=…`. That would be the fallback source of bounds if the prototype hook ever breaks. It is not implemented, because the hook works.
- Content scripts run in an isolated world and cannot see `google.maps`, so the overlay runs as a second content script with `world: "MAIN"`. The isolated script sends it the hex polygons and values through `window.postMessage`. The data is public, so it's fine that the page can read these messages.

### Other observations
- Without consent, Idealista shows a Didomi consent banner. The extension does not touch it.
- No script CSP is set (only `frame-ancestors`), which doesn't affect a content script.
- Below roughly 1000 px width, Idealista serves a narrow layout without the map.

## How the extension was tested

The packaged extension cannot be installed in the in-app test browser, and that browser blocks public pages from loading `127.0.0.1` scripts. So the pieces were tested separately:
- **Badges, fallback, filter, settings:** `extension/test/fixture.html` rebuilds Idealista's list markup, including an inline `config.listingMultimediaCarrousels` script, from 8 real listings captured on 2026-10-21, plus synthetic cases: no coordinates (→ barri `approx`), a point outside the city (→ no badge), an ad card, and a card inserted 1.5 s after load. The real content-script bundle runs there on a fake `chrome.storage`. All cases behaved as expected. Hide-red, crime-only re-weighting and all-categories-off were checked by changing settings live.
- **Map overlay:** the built `page.js` was injected into the live `/mapa-google` page, together with 45 real hexes around Plaça Catalunya. It captured the map instance as soon as the map rendered, attached the overlay and legend, and stayed aligned through pan and zoom from 12 down to about 18 (La Rambla at street level).
- **Popup:** the built popup was rendered with the fake storage. Checkbox, slider, hide-red and reset all write the expected settings, and the date ranges, caveats and sources show up.

## Known limitations

- Complaints measure **reporting behaviour**: tourist, central and higher-engagement areas report more, and some low-income areas report less.
- Normalising by **residents** penalises areas with many visitors and few residents (old town, Vila Olímpica, Diagonal Mar). Footfall data would be better but is not openly available.
- **Crime is district-level**, split into barris by an estimate. It is never hex-level.
- **Freshness:** IRIS lags by ~6 months (the newest file ends 2026-03), Mossos by ~1 month, and GUB is annual (2025 is the newest year). Income is from 2023.
- The 2017–2019 cleaning-hotspot datasets are not used because they are stale.
- H3 comes from an experimental (`x/`) package pinned to a master commit. It is checked against the reference in tests.
- Extension: listing coordinates come from the static-map URL inside an inline script. If Idealista changes that, cards fall back to barri averages taken from the title, and some Idealista zone names (e.g. "Diagonal Mar - La Mar Bella") don't match an official barri, so those cards get no badge.
- Extension: the map hook depends on `google.maps.Map.prototype.get` being called by the map instance. If Google changes that, the overlay silently doesn't appear (look for `[BCN-SQ]` in the console). Bounds from the page's `listingmapajaxnopins.ajax` requests are the documented fallback, but they are not implemented.
- Extension: while dragging, the overlay canvas covers only the previous viewport until the map goes idle, so you can see a blank edge for a moment.
- Extension: the tooltip names the hex's majority barri, which near a border can differ from the barri in Idealista's title.

# Street Quality Spain — working notes

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

## Spain-wide layer (outside Barcelona)

Added 2026-10-21 so the extension works on any Idealista search in Spain. The Barcelona grid above stays as it is; everywhere else (including Barcelona's suburbs) the extension uses this layer. Code: [`pipeline/spain.go`](pipeline/spain.go), [`extension/src/spain.ts`](extension/src/spain.ts).

### What exists nationally

Barcelona's complaint layers (dirt, smell, graffiti, incivism) come from the city's own IRIS register, which has no national counterpart. Some big cities publish their own complaint registers, but they need a separate pipeline each. What does cover the whole country:

| Layer | Source | How it is fetched | Granularity | In the current build |
|---|---|---|---|---|
| Street crime | Ministerio del Interior, *Balance de Criminalidad* (Portal Estadístico de Criminalidad) | newest 4th-quarter release listed on `balances.html`, municipal table (`…Municipios mayores de 20.000 habitantes e islas`) as `csv_bdsc` from the JAXI export (`/sec/jaxiPx/files/_px/es/csv_bdsc/DatosBalanceAnt/l0/<file>.csv_bdsc`) | municipality, **only those over 20,000 residents** (427 of 8,134, 70% of residents), yearly | 2025 (table 1509012) |
| Income, residents | INE, *Atlas de Distribución de Renta de los Hogares* (operation 353) | table list from `servicios.ine.es/wstempus/js/ES/TABLAS_OPERACION/353`, tables as `jaxiT3/files/t/es/csv_bdsc/<id>.csv`. The list has one table per province plus one for all of Spain (30824 income, 30832 demographics, ~400 MB each); the pipeline stops once a table covers the whole country | census section (36,457), municipality | income 2024, population 2024 |
| Section boundaries | INE, *Cartografía digitalizada de secciones censales* | `ine.es/prodyser/cartografia/seccionado_<year>.zip`, shapefile in ETRS89 / UTM 30 for all of Spain (the Canaries too) | census section | 1 January 2025 (matches ADRH 2024: 100% of codes) |

Crime types counted: Balance types 1–10 (homicide and attempts, assault, kidnapping, sexual offences, robbery with violence, burglary, theft, vehicle theft, drug trafficking). "11. Resto de criminalidad convencional" and cybercrime are left out, as fraud is in Barcelona. Catalonia, the Basque Country and Navarre are included: the Balance carries Mossos, Ertzaintza and Policía Foral figures.

### Method

1. **Crime rate** = street crimes in the year ÷ municipal residents (ADRH). Percentile is population-weighted across the 427 municipalities: 76 for Madrid means 76% of the residents covered live somewhere with a lower rate. One value per municipality.
2. **Income** = net income per person per census section. Percentile population-weighted across all sections and inverted for the score (0 = highest income).
3. **Score** = 100 − weighted mean of the two, default weights crime 35, income 15 (popup-adjustable, income can be switched off). Without a crime figure, the score is income alone.
4. **Location index:** the boundaries are converted from UTM 30 with Krüger's series ([`pipeline/utm.go`](pipeline/utm.go); checked against Barcelona's own WGS84 section geometry: mean shift 0.5 m, 98.5% of points in the same section). Every H3 res-9 cell goes to the section containing its centre (sections smaller than a cell get res-10 cells). Where every section under a coarser cell (res 5–8) is at least 4× that cell's area, i.e. in the countryside, the whole coarse cell goes to the section covering most of it. Result: 338,241 cells instead of 1.4 million for a plain compacted res-9 index.
5. **Shards:** cells are grouped by their res-3 ancestor into 78 files (`data/spain/<cell>.json`, 8.5 MB in total, the largest 600 KB). Each holds its sections as `[municipality, € per person, income percentile, residents]`, the municipalities they belong to, and `cell → section`. `index.json` has the metadata and the shard list. The content script fetches only the shard a listing or the visible map falls in.
6. **Names:** INE writes "Palmas de Gran Canaria, Las"; the pipeline moves the article back ("Las Palmas de Gran Canaria").

### Sanity check

Full table in [`data/spain/summary.md`](data/spain/summary.md) (municipalities over 100,000 residents). Lowest street crime per 1,000 residents: Algeciras 10.3, San Cristóbal de La Laguna 11.1, Fuenlabrada 11.2, Oviedo 11.7. Highest: Barcelona 62.3, Palma 44.9, L'Hospitalet 39.0, València 36.8, Bilbao 36.3, Marbella 33.8. Madrid 28.8 (percentile 76). As in Barcelona, per-resident rates inflate tourist and central cities. On the Madrid map, Pozuelo comes out green and Puente de Vallecas red, with the centre in between.

## Idealista integration findings

Inspected 2026-10-21 in a desktop-width browser (1440×900), on `/alquiler-viviendas/barcelona-barcelona/` and its map view. All selectors live in [`extension/src/selectors.ts`](extension/src/selectors.ts).

### List view (`/alquiler-viviendas/barcelona-barcelona/`, `…/pagina-N.htm`)
- Server-rendered. Pagination does full page loads (`pagina-2.htm`); there is no infinite scroll. A MutationObserver still watches for cards that get re-rendered.
- Card: `article.item[data-element-id]`. `data-element-id` is the listing ID. Ads are `article.adv` and have no ID.
- Title link: `a.item-link` (its `title` attribute is e.g. `Piso en Calle de Mallorca, L'Antiga Esquerra de l'Eixample, Barcelona`). The text sits in `.item-info-container`, and the price is in `.price-row`.
- **Coordinates:** there are no `lat`/`lng` attributes in the markup. However, an inline `<script>` sets `var config = {…, listingMultimediaCarrousels: {"<id>": {…, "map": {"src": "https://maps.googleapis.com/maps/api/staticmap?…&center=41.37568530%2C2.16067930&…", "addressVisibility": "HIDDEN" | "EXACT"}}}}`. That's the "Mapa de la zona" slide of each card's carousel. The extension reads the `center=` parameter from the script **text** in the DOM. It does not need page globals and makes no requests. On the sample page **30 of 30** listings had it.
- `addressVisibility: HIDDEN` means the advertiser hid the exact address, so the point is approximate. Those badges are marked `≈`.
- Fallback when a card has no map entry: take the barri from the title (`…, <barri>, Barcelona`), match it to the grid's barri names (accent- and case-insensitive, plus a small alias table), use the barri average and mark the badge `approx`.

### Listing detail page (`/inmueble/<id>/`)
- Inspected 2026-10-21 on `/inmueble/112873935/` (El Raval, address hidden).
- **Coordinates:** the inline `var config = {…}` script has `multimediaCarrousel: {…, "map": {"src": "…staticmap?…&center=41.38202860%2C2.16966330&…&channel=map_detail…", "addressVisibility": "HIDDEN"}}`: the same static-map slide as on list cards, for the one listing. The value is JSON, so the same parser reads it. `latitude: ''` / `longitude: ''` also appear in the page but are empty.
- Header: `section.detail-content-wrapper` holds `.main-info__title` (title, then `.main-info__title-minor` = `El Raval, Barcelona`) and then `div.info-data` (price, m², rooms). The panel goes right after that `div.info-data`. A second `.main-info` with its own `.info-data` is the sticky bar, which is not a child of the wrapper.
- Fallback without coordinates: the barri from `.main-info__title-minor`, matched like card titles.

### Map view (`/alquiler-viviendas/barcelona-barcelona/mapa-google`)
- Note that `/mapa` (without `-google`) is a different page: a static district-picker image ("greymaps"), not the listings map.
- The listings map is **Google Maps JS API 3.65 ("quarterly")**. It is loaded by Idealista's React component through `@vis.gl/react-google-maps` (`solution_channel=GMP_visgl_rgmlibrary`, `callback=google.maps.__ib__`), uses raster rendering with a cloud `map_id`, and has 0 tilt and 0 heading. The container is `.google-map-container` inside `.multizone-map-container`.
- No global holds the map instance. **Hook:** in the page's main world, wrap a method on `google.maps.Map.prototype` that the instance calls constantly (`get`, which comes from MVCObject; it was called about 270 times while the map loaded). The first call with `this instanceof google.maps.Map` hands over the instance. Verified live: zoom 12, bounds OK.
- **Overlay:** a `google.maps.OverlayView` subclass draws one `<canvas>` in the `overlayLayer` pane, projecting hex vertices with `fromLatLngToDivPixel`. Google calls `draw()` on every pan and zoom, so the overlay stays in sync with no extra listeners. Verified live with a test dot on the old town.
- When the viewport changes, the page fetches `/ajax/listingcontroller/listingmapajaxnopins.ajax?…&zoom=12&northEast=…&southWest=…`. That would be the fallback source of bounds if the prototype hook ever breaks. It is not implemented, because the hook works.
- Content scripts run in an isolated world and cannot see `google.maps`, so the overlay runs as a second content script with `world: "MAIN"`. The isolated script sends it the hex polygons and values through `window.postMessage`. The data is public, so it's fine that the page can read these messages.
- For the Spain-wide layer the map script also posts the viewport (`view`: bounds and zoom) on capture and on every `idle` event. The content script answers with hexes covering the view, sized to the zoom (res = zoom − 4, between 5 and 9, at most 3,000 hexes; none below zoom 8), and leaves out hexes Barcelona's grid already covers. Verified live on `/alquiler-viviendas/madrid-madrid/mapa-google`: the hook caught the instance and posted views at zoom 11, 6 and 7 while zooming.
- The shards are listed in the manifest's `web_accessible_resources` for `https://www.idealista.com/*`, because a content script's `fetch` of its own extension files needs that.

### Other observations
- Without consent, Idealista shows a Didomi consent banner. The extension does not touch it.
- No script CSP is set (only `frame-ancestors`), which doesn't affect a content script.
- Below roughly 1000 px width, Idealista serves a narrow layout without the map.

## How the extension was tested

The packaged extension cannot be installed in the in-app test browser, and that browser blocks public pages from loading `127.0.0.1` scripts. So the pieces were tested separately:
- **Badges, fallback, filter, settings:** `extension/test/fixture.html` rebuilds Idealista's list markup, including an inline `config.listingMultimediaCarrousels` script, from 8 real listings captured on 2026-10-21, plus synthetic cases: no coordinates (→ barri `approx`), a point outside the city (L'Hospitalet → `Area` badge), an ad card, and a card inserted 1.5 s after load. Spain-wide cases: Madrid (`Area 38`), Seville with the address hidden (`Area 39 ≈`), Las Palmas (`Area 67`, which also checks the Canaries projection), Cadaqués (no crime figure: `Area 70` from income alone, no badge with income switched off) and a point at sea (no badge). The real content-script bundle runs there on a fake `chrome.storage`. All cases behaved as expected. Hide-red, crime-only re-weighting and all-categories-off were checked by changing settings live.
- **Detail panel:** `extension/test/detail.html` rebuilds the detail header and its `config.multimediaCarrousel` script; `?case=` switches between the real Raval listing (address hidden), two exact points (Tres Torres, Sagrada Família), no coordinates (→ barri average from the location line) and a point outside the city (→ Spain-wide panel: safety and income rows; a copy moved to Cadaqués shows "No data" for safety). All behaved as expected, as did crime-only, all-layers-off and badges-off from the settings. The panel's selectors and the coordinate parse were checked on the live page; the bundle itself was not run there.
- **Map overlay:** the built `page.js` was injected into the live `/mapa-google` page, together with 45 real hexes around Plaça Catalunya. It captured the map instance as soon as the map rendered, attached the overlay and legend, and stayed aligned through pan and zoom from 12 down to about 18 (La Rambla at street level).
- **Spain-wide map layer:** `view` messages were posted to the fixture page and the overlay answer drawn on a canvas: about 500 hexes for Madrid at zoom 12–13, none at zoom 6, Barcelona's suburbs added around the city grid. The drawing code is unchanged from the live test above; the Spain-wide hexes were not drawn on the live map, because the test browser cannot load the shards into a public page.
- The in-app test browser runs hidden, where `requestAnimationFrame` never fires, so cards added after load were not re-rendered there. The checks used a copy of the fixtures with `requestAnimationFrame` replaced by a 16 ms timer (`dist-test/*-raf.html`, not committed).
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
- Spain-wide layer: **crime is one figure per municipality** and exists only for municipalities over 20,000 residents; smaller places are scored on income alone. Income is a context indicator, not a measure of street quality.
- Spain-wide layer: the countryside index assigns a coarse hex to the section covering most of it, so near borders between large rural sections a point can get the neighbouring section's income.
- Spain-wide layer: cards without coordinates get no badge outside Barcelona (there is no title fallback).
- Spain-wide layer: the newest crime year is read from the "Anteriores" list of the Interior portal; a 4th quarter that is still the current release appears there only once the next quarter is out.

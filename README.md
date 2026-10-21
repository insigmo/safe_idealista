# Street Quality Spain (formerly BCN Street Quality)

A personal Chrome extension that overlays neighbourhood-quality data on Idealista listings across Spain. Barcelona gets the detailed layers: dirt, smell, graffiti, incivism and crime. The rest of Spain gets street crime per municipality (Interior Ministry, municipalities over 20,000 residents) and household income per census section (INE). The data comes from official open data, is bundled with the extension, and the extension makes no remote calls.

- `pipeline/` — Go program that downloads the open data and builds `data/grid.json` (Barcelona: H3 res-9 cells + barri averages) and `data/spain/` (the Spain-wide layer as H3 shards).
- `data/` — generated output: `grid.json`, the category mapping report, barri rankings, a Leaflet review map, and `spain/` (shards, `index.json`, `summary.md` with the municipal crime ranking).
- `extension/` — Manifest V3 extension (TypeScript + esbuild + h3-js); unpacked build in `extension/dist/`.
- [`NOTES.md`](NOTES.md) — dataset IDs, category mapping, method, caveats.

## Rebuild the data

You need Go 1.26+. No C compiler is required.

```bash
cd pipeline && make data
```

Without `make` (e.g. plain Windows):

```bash
cd pipeline && go run . -cache .cache -out ../data
```

`make bcn` / `make spain` (or `-only bcn` / `-only spain`) builds one layer. Downloads are cached in `pipeline/.cache/`: ~150 MB for Barcelona, ~500 MB for the Spain-wide layer (INE's national income and demographic tables are ~400 MB each, plus 60 MB of census-section boundaries). Use `make refresh` or `-refresh` to download them again; INE tables are also re-downloaded on their own when INE updates them. Other flags: `-months 24`, `-res 9`, `-crime-gub-alpha 0.5`. Tests: `make test`.

## Look at the result

The review map loads Leaflet and OpenStreetMap tiles, so serve it over HTTP:

```bash
python -m http.server 8765 --directory data
```

Then open http://localhost:8765/review_map.html.

## Load the extension

A built copy is committed in `extension/dist/`.

1. Open `chrome://extensions` and switch on **Developer mode**.
2. Click **Load unpacked** and pick `extension/dist`.
3. Open any Idealista search, e.g. https://www.idealista.com/alquiler-viviendas/barcelona-barcelona/ or https://www.idealista.com/alquiler-viviendas/madrid-madrid/. Each listing gets a badge (hover it for the breakdown). Opening a listing (`/inmueble/<id>/`) shows a panel under the price with the overall score, a verdict (good / average / poor) and a level for dirt, graffiti, safety, smell and incivism, from the same data as the map. The map view (**Mapa** → `/mapa-google`) gets a hex overlay with a legend in the bottom-left corner.
4. The toolbar icon opens the popup: layer toggles, weights, opacity, "hide red listings", legend, data date ranges, caveats and sources.

Badges read: `Street 72` in Barcelona and `Area 64` elsewhere in Spain (green ≥ 60, yellow 40–59, red < 40). The two scales are not comparable: `Street` ranks within Barcelona on five layers, `Area` ranks within Spain on municipal crime and section income. Places under 20,000 residents have no crime figure, so their `Area` score is income alone (switch income off in the popup and they get no badge). `≈` means the advertiser hid the exact address, so the point is approximate. `approx` means the card had no coordinates and the barri average is shown (Barcelona only).

## Rebuild the extension

You need Node 20+.

```bash
cd extension && npm ci && npm run build
```

The build bundles `data/grid.json` into `dist/content.js` and copies `data/spain/` to `dist/spain/` (read on demand as web-accessible resources), so rebuild the extension after `make data`. `npm run typecheck` runs TypeScript.

For development, `npm run build:harness` writes `dist-test/`:
- `fixture.html` is a local copy of Idealista's list markup, built from real listings, with the content script running on a fake `chrome.storage`. It also has synthetic listings in Madrid, Seville, Las Palmas, Cadaqués and at sea for the Spain-wide layer.
- `popup.html` previews the popup.

Serve the folder with `python -m http.server 8766 --directory extension/dist-test` and call `window.__bcnsqSet({hideRed: true})` in the fixture's console to change settings.

The extension only reads what Idealista already rendered and the data shipped inside it. It makes no network requests, stores only your settings (`chrome.storage.sync`) and has no analytics. Idealista-specific selectors are all in `extension/src/selectors.ts`. If something breaks, look for `[BCN-SQ]` in the page console.

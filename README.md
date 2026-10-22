# Safe Idealista (formerly Street Quality Spain, BCN Street Quality)

A personal Chrome and Firefox extension that overlays neighbourhood-quality data on Idealista listings across Spain. Barcelona gets the detailed layers: dirt, smell, graffiti, incivism and crime. Madrid gets the same except smell, which the city does not publish. The rest of Spain gets street crime per municipality (Interior Ministry, municipalities over 20,000 residents) and household income per census section (INE). The data comes from official open data. A copy ships with the extension, and a monthly GitHub Actions run republishes it so installed extensions pick up fresh data without an update.

- `pipeline/` — Go program that downloads the open data and builds `data/grid.json` (Barcelona: H3 res-9 cells + barri averages), `data/madrid/grid.json` (the same for Madrid) and `data/spain/` (the Spain-wide layer as H3 shards).
- `data/` — generated output: `grid.json`, the category mapping report, barri rankings, a Leaflet review map, `madrid/` (grid, mapping report, barrio rankings) and `spain/` (shards, `index.json`, `summary.md` with the municipal crime ranking).
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

`make bcn` / `make madrid` / `make spain` (or `-only bcn|madrid|spain`) builds one layer. Downloads are cached in `pipeline/.cache/`: ~150 MB for Barcelona, ~450 MB of Madrid citizen reports (plus the INE files below, which Madrid also uses), ~500 MB for the Spain-wide layer (INE's national income and demographic tables are ~400 MB each, plus 60 MB of census-section boundaries). Use `make refresh` or `-refresh` to download them again; INE tables are also re-downloaded on their own when INE updates them. Other flags: `-months 24`, `-res 9`, `-crime-gub-alpha 0.5`. Tests: `make test`.

## Monthly data releases

`.github/workflows/data-release.yml` runs on the 3rd of each month (or by hand: Actions → Data release → Run workflow). It runs `make data` against fresh downloads, refuses output with far fewer cells or shards than today, packs it with `pack.mjs` and force-pushes it as the only commit of the `data-release` branch.

The extension's background worker (`extension/src/background.ts`) reads `release.json` from that branch once a day via raw.githubusercontent.com. When the release is newer than the data in use, it downloads only the files that differ from the bundled copy, checks each against its SHA-256, and stores them in `chrome.storage.local` (`unlimitedStorage`). The release becomes active once every file is in place, from the next page load. The popup shows the data release in use and has a "Check for updates" button. A release with a different `schema` (see `pack.mjs` and `src/data.ts`) is ignored, so bump it when the data format changes incompatibly.

## Look at the result

The review map loads Leaflet and OpenStreetMap tiles, so serve it over HTTP:

```bash
python -m http.server 8765 --directory data
```

Then open http://localhost:8765/review_map.html.

## Load the extension

Built copies are committed in `extension/dist/` (Chrome) and `extension/dist-firefox/` (Firefox 140+, desktop; Android 142+).

1. Chrome: open `chrome://extensions`, switch on **Developer mode**, click **Load unpacked** and pick `extension/dist`.
2. Firefox: open `about:debugging#/runtime/this-firefox`, click **Load Temporary Add-on…** and pick `extension/dist-firefox/manifest.json`. A temporary add-on is removed when Firefox closes; a permanent install needs the AMO-signed build (see below).
3. Open any Idealista search, e.g. https://www.idealista.com/alquiler-viviendas/barcelona-barcelona/ or https://www.idealista.com/alquiler-viviendas/madrid-madrid/. Each listing gets a badge (hover it for the breakdown). Opening a listing (`/inmueble/<id>/`) shows a panel under the price with the overall score, a verdict (good / average / poor) and a level for each layer (in Barcelona dirt, graffiti, safety, smell and incivism; in Madrid the same without smell; elsewhere safety and income), from the same data as the map. The map view (**Mapa** → `/mapa-google`) gets a hex overlay with a legend in the bottom-left corner.
4. The toolbar icon opens the popup: your address, layer toggles, weights, opacity, "hide red listings", legend, data date ranges, caveats and sources.
5. With an address set (e.g. `Carrer de Mallorca 401, Barcelona`, or coordinates like `41.39, 2.17`), every listing shows its straight-line distance from it, and Idealista's **Ordenar** dropdown gets **Nearest to my address**. Idealista sorts on its server and has no such order, so the extension reorders the 30 listings of the page you are on (ads keep their place, listings without coordinates go last). The choice lasts for the tab, page 2 onwards included, until you pick one of Idealista's own orders.

Badges read: `Street 72` in Barcelona and Madrid and `Area 64` elsewhere in Spain (green ≥ 60, yellow 40–59, red < 40). The scales are not comparable: a `Street` score ranks within its own city (Barcelona on five layers, Madrid on four), `Area` ranks within Spain on municipal crime and section income. Places under 20,000 residents have no crime figure, so their `Area` score is income alone (switch income off in the popup and they get no badge). `≈` means the advertiser hid the exact address, so the point is approximate. `approx` means the card had no coordinates and the barri average is shown (Barcelona only).

## Rebuild the extension

You need Node 20+.

```bash
cd extension && npm ci && npm run build
```

The build packs `data/` into `dist/` with `extension/pack.mjs`: `bcn/` and `madrid/` (grid + meta), `spain/` (index + shards) and `release.json`, which lists every file with its SHA-256. They are read on demand as web-accessible resources, so rebuild the extension after `make data`. `npm run typecheck` runs TypeScript.

The same build writes `dist-firefox/`: the files of `dist/` with the manifest rewritten for Firefox (an event-page `background.scripts`, the add-on id, and `data_collection_permissions` declaring the address sent to Nominatim). `npm run lint:firefox` runs Mozilla's `web-ext lint` over it.

`npm run package` builds, lints and writes the store uploads to `extension/packages/`: `chrome-<version>.zip` for the Chrome Web Store and `firefox-<version>.zip` for addons.mozilla.org. AMO also asks for the source, since the scripts are bundled and minified: upload `git archive --format zip -o source.zip HEAD` with the build steps above. Bump `version` in `src/manifest.json` before each upload.

GitHub builds the same files (`.github/workflows/extension-package.yml`): every run keeps `chrome-<version>.zip`, `firefox-<version>.zip` and `source-<version>.zip` as the **extension-packages** artifact of the run. To cut a release, bump the version, commit, then tag and push it; the run attaches the three zips to a GitHub release:

```bash
git tag v0.4.1 && git push origin v0.4.1
```

Upload `chrome-*.zip` to the Chrome Web Store, and `firefox-*.zip` to AMO with `source-*.zip` as its source code.

For development, `npm run build:harness` writes `dist-test/`:
- `fixture.html` is a local copy of Idealista's list markup, built from real listings, with the content script running on a fake `chrome.storage`. It also has synthetic listings in Madrid (city grid), Pozuelo, Seville, Las Palmas, Cadaqués and at sea (Spain-wide layer).
- `popup.html` previews the popup.

Serve the folder with `python -m http.server 8766 --directory extension/dist-test` and call `window.__bcnsqSet({hideRed: true})` in the fixture's console to change settings.

The extension only reads what Idealista already rendered and the data shipped inside it. It makes two kinds of network request: the address lookup (OpenStreetMap Nominatim, Spain only), sent from the popup when you press Set (typed coordinates skip it), and the daily data-release check against raw.githubusercontent.com, which sends nothing about you. It stores your settings, address included (`chrome.storage.sync`), and downloaded data (`chrome.storage.local`), and has no analytics. Idealista-specific selectors are all in `extension/src/selectors.ts`. If something breaks, look for `[BCN-SQ]` in the page console.

## Licence

Code: GPL v3 ([LICENSE](LICENSE)). The data comes from public open-data portals and keeps their terms; the sources, attributions and the no-warranty disclaimer are in [NOTICE.md](NOTICE.md).

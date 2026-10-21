# BCN Street Quality

A personal Chrome extension (in progress) that overlays neighbourhood-quality data on Idealista listings in Barcelona: dirt, smell, graffiti, incivism and crime. The data comes from official open data, is bundled with the extension, and the extension makes no remote calls.

- `pipeline/` — Go program that downloads the open data and builds `data/grid.json` (H3 res-9 cells + barri averages).
- `data/` — generated output: `grid.json`, the category mapping report, barri rankings, and a Leaflet review map.
- `extension/` — Manifest V3 extension (Part 2, not built yet).
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

Downloads (~150 MB) are cached in `pipeline/.cache/`. Use `make refresh` or `-refresh` to download them again. Other flags: `-months 24`, `-res 9`, `-crime-gub-alpha 0.5`. Tests: `make test`.

## Look at the result

The review map loads Leaflet and OpenStreetMap tiles, so serve it over HTTP:

```bash
python -m http.server 8765 --directory data
```

Then open http://localhost:8765/review_map.html.

## Load the extension

Not built yet (Part 2).

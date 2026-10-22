// Spain-wide layer, used outside Barcelona's grid: street crime per
// municipality and household income per census section. The data ships with
// the extension as shards (dist/spain/<res-3 H3 cell>.json, see
// pipeline/spain.go) that are read on demand, a newer downloaded copy
// winning (data.ts); nothing leaves the browser.
import { cellToBoundary, cellToLatLng, cellToParent, latLngToCell, polygonToCells } from 'h3-js';
import { readData } from './data';
import type { Meta, Values } from './grid';

export interface Muni {
  n: string; // name
  pop: number;
  inc?: number; // € per person, whole municipality
  crime?: number; // percentile, 0 = safest; absent below 20,000 residents
  c1k?: number; // street crimes per 1,000 residents per year
  cn?: number; // street crimes in the year
}

// [municipality code, € per person, income percentile (0 = richest), residents]
type SectionRow = [string, number | null, number | null, number];

interface Shard {
  m: Record<string, Muni>;
  s: SectionRow[];
  c: Record<string, number>; // compacted H3 cell -> index into s
}

interface Index {
  meta: Meta;
  cell_res: number;
  shard_res: number;
  shards: string[];
}

export interface Place {
  muni: Muni;
  income?: number;
  values: Values;
}

const load = readData;

let index: Promise<{ ix: Index; keys: Set<string> } | null> | null = null;
let loadedMeta: Meta | null = null;
const shards = new Map<string, Promise<Shard | null>>();
const ready = new Map<string, Shard | null>();

function getIndex(): Promise<{ ix: Index; keys: Set<string> } | null> {
  index ??= load<Index>('spain/index.json').then((ix) => {
    if (!ix) return null;
    loadedMeta = ix.meta;
    return { ix, keys: new Set(ix.shards) };
  });
  return index;
}

function getShard(key: string): Promise<Shard | null> {
  let p = shards.get(key);
  if (!p) {
    p = load<Shard>(`spain/${key}.json`).then((s) => {
      ready.set(key, s);
      return s;
    });
    shards.set(key, p);
  }
  return p;
}

// The index's meta once loaded (date ranges for notes), else null.
export function spainMeta(): Meta | null {
  return loadedMeta;
}

// The pipeline files each stored cell under its res-3 ancestor in the H3
// hierarchy, which near shard edges is not the res-3 cell containing the
// point, so always derive the key from the finest cell.
function located(ix: Index, lat: number, lng: number): { fine: string; key: string } {
  const fine = latLngToCell(lat, lng, ix.cell_res + 1);
  return { fine, key: cellToParent(fine, ix.shard_res) };
}

// Sections are stored as compacted cells, so walk from the finest
// resolution up until a stored ancestor turns up.
function lookup(sh: Shard, ix: Index, fine: string): Place | null {
  for (let r = ix.cell_res + 1; r >= ix.shard_res; r--) {
    const i = sh.c[r === ix.cell_res + 1 ? fine : cellToParent(fine, r)];
    if (i === undefined) continue;
    const [code, inc, bad] = sh.s[i];
    const muni = sh.m[code];
    if (!muni) return null;
    const values: Values = {};
    if (muni.crime !== undefined) values.crime = muni.crime;
    if (bad !== null) values.income = bad;
    return { muni, income: inc ?? undefined, values };
  }
  return null;
}

export async function placeAt(lat: number, lng: number): Promise<Place | null> {
  const got = await getIndex();
  if (!got) return null;
  const { fine, key } = located(got.ix, lat, lng);
  if (!got.keys.has(key)) return null;
  const sh = await getShard(key);
  return sh ? lookup(sh, got.ix, fine) : null;
}

export interface Bounds { north: number; south: number; east: number; west: number }

const MAX_CELLS = 3000;

// Hexes covering the map view, sized to the zoom level, with the values at
// each hex centre; `skip` drops hexes another layer already covers. Empty
// when zoomed out too far for this layer to be useful.
export async function cellsInView(
  b: Bounds,
  zoom: number,
  skip: (lat: number, lng: number) => boolean,
): Promise<{ ring: number[]; values: Values }[]> {
  const got = await getIndex();
  if (!got || zoom < 8) return [];
  const box = [[b.north, b.west], [b.north, b.east], [b.south, b.east], [b.south, b.west]];
  let res = Math.min(got.ix.cell_res, Math.max(5, Math.round(zoom) - 4));
  let ids = polygonToCells(box, res);
  while (ids.length > MAX_CELLS && res > 5) ids = polygonToCells(box, --res);
  if (ids.length > MAX_CELLS) return [];
  const at = ids.flatMap((id) => {
    const [lat, lng] = cellToLatLng(id);
    return skip(lat, lng) ? [] : [{ id, ...located(got.ix, lat, lng) }];
  });
  await Promise.all([...new Set(at.map((a) => a.key))].filter((k) => got.keys.has(k)).map(getShard));
  const out = [];
  for (const { id, fine, key } of at) {
    const sh = ready.get(key);
    const p = sh ? lookup(sh, got.ix, fine) : null;
    if (p) out.push({ ring: cellToBoundary(id).flat(), values: p.values });
  }
  return out;
}

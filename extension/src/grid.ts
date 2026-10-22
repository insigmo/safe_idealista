import { cellToBoundary, latLngToCell } from 'h3-js';
import { readData } from './data';
import type { Category, Settings } from './settings';
import { CATEGORIES } from './settings';

export interface Raw { dirt: number; smell: number; graffiti: number; incivism: number }

export interface Cell {
  dirt: number; smell: number; graffiti: number; incivism: number; crime: number;
  score: number; pop: number; barri: number; inc?: number; lp?: boolean; n_raw: Raw;
}

export interface Barri {
  name: string; district: string; pop: number;
  dirt: number; smell: number; graffiti: number; incivism: number; crime: number;
  score: number; inc?: number; crime_est: number; crime_per_1k_year: number; n_raw: Raw;
}

export interface Meta {
  generated_at: string;
  h3_resolution: number;
  date_ranges: Record<string, { from: string; to: string }>;
  caveats: string[];
  method: string[];
  sources: { portal: string; dataset: string; title: string; resource: string }[];
}

interface GridFile { meta: Meta; cells: Record<string, Cell>; barris: Record<string, Barri> }

// Set by loadGrid(), which the content script awaits before anything else.
export let grid: GridFile;

export async function loadGrid(): Promise<void> {
  const g = await readData<GridFile>('bcn/grid.json');
  if (!g) throw new Error('Barcelona grid missing');
  grid = g;
  barriWords = Object.values(g.barris).map((b) => ({ b, w: words(b.name) }));
  polygons = null;
}

// Percentiles (0 best … 100 worst) of the layers that exist at a location.
export type Values = Partial<Record<Category, number>>;

// Weighted badness (0 best … 100 worst) over the enabled categories that have
// a value here, turned into a score where 100 is best. Null when none is left.
export function score(v: Values, s: Settings): number | null {
  let sum = 0;
  let wsum = 0;
  for (const c of CATEGORIES) {
    const x = v[c];
    if (x === undefined || !s.enabled[c] || s.weights[c] <= 0) continue;
    sum += s.weights[c] * x;
    wsum += s.weights[c];
  }
  return wsum === 0 ? null : Math.round(100 - sum / wsum);
}

export type Level = 'good' | 'mid' | 'bad';

export function level(score: number): Level {
  return score >= 60 ? 'good' : score >= 40 ? 'mid' : 'bad';
}

// Green → yellow → red for badness 0..1, as in the review map.
export function ramp(badness: number, alpha = 1): string {
  const t = Math.max(0, Math.min(1, badness));
  return `hsla(${Math.round(120 * (1 - t))},75%,45%,${alpha})`;
}

export function cellAt(lat: number, lng: number): { id: string; cell: Cell } | null {
  const id = latLngToCell(lat, lng, grid.meta.h3_resolution);
  const cell = grid.cells[id];
  return cell ? { id, cell } : null;
}

export function barriOf(cell: Cell): Barri | undefined {
  return grid.barris[String(cell.barri)];
}

// Barri names on Idealista differ from the official ones in articles, hyphens
// and the odd word ("El Gòtic" vs "el Barri Gòtic"), so match on word sets.
const STOP = new Set(['el', 'la', 'els', 'les', 'l', 'i', 'de', 'del', 'd', 'barri']);

function words(s: string): Set<string> {
  const folded = s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
  return new Set(folded.split(/[^a-z0-9]+/).filter((w) => w && !STOP.has(w)));
}

let barriWords: { b: Barri; w: Set<string> }[] = [];

export function barriByName(name: string): Barri | null {
  const q = words(name);
  if (q.size === 0) return null;
  let best: Barri | null = null;
  let bestScore = 0;
  for (const { b, w } of barriWords) {
    let inter = 0;
    for (const x of q) if (w.has(x)) inter++;
    const jaccard = inter / (q.size + w.size - inter);
    if (jaccard > bestScore) {
      best = b;
      bestScore = jaccard;
    }
  }
  return bestScore >= 0.6 ? best : null;
}

let polygons: { id: string; cell: Cell; ring: number[] }[] | null = null;

// Hex polygons for the map overlay, flattened [lat, lng, lat, lng, …]; built once.
export function cellPolygons(): { id: string; cell: Cell; ring: number[] }[] {
  polygons ??= Object.entries(grid.cells).map(([id, cell]) => ({ id, cell, ring: cellToBoundary(id).flat() }));
  return polygons;
}

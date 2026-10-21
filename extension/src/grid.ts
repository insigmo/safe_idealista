import { cellToBoundary, latLngToCell } from 'h3-js';
import gridJson from '../../data/grid.json';
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

export const grid = gridJson as unknown as GridFile;

export type Values = Pick<Cell, Category>;

// Weighted badness (0 best … 100 worst) over the enabled categories, turned
// into a score where 100 is best. Null when every category is switched off.
export function score(v: Values, s: Settings): number | null {
  let sum = 0;
  let wsum = 0;
  for (const c of CATEGORIES) {
    if (!s.enabled[c] || s.weights[c] <= 0) continue;
    sum += s.weights[c] * v[c];
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

const barriWords = Object.values(grid.barris).map((b) => ({ b, w: words(b.name) }));

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

// Hex polygons for the map overlay, flattened [lat, lng, lat, lng, …].
export const cellPolygons: { id: string; cell: Cell; ring: number[] }[] = Object.entries(grid.cells).map(
  ([id, cell]) => ({ id, cell, ring: cellToBoundary(id).flat() }),
);

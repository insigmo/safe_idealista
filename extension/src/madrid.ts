// Madrid's own grid (pipeline/madrid.go): dirt and graffiti per hex from
// citizen reports, incivism and crime per barrio from city-police incidents.
// Madrid publishes nothing on smell, so there is no smell layer. The file
// ships with the extension (dist/madrid/grid.json) and is read on demand,
// only for points near Madrid; a newer downloaded copy wins (data.ts).
import { cellToBoundary, latLngToCell } from 'h3-js';
import { readData } from './data';
import type { Meta } from './grid';

export interface MadridCell {
  dirt: number; graffiti: number; incivism: number; crime: number;
  score: number; pop: number; barri: number; inc?: number; lp?: boolean;
  n_raw: { dirt: number; graffiti: number };
}

export interface MadridBarrio {
  name: string; district: string; pop: number;
  dirt: number; graffiti: number; incivism: number; crime: number; score: number;
  inc?: number; crime_n: number; incivism_n: number; crime_per_1k_year: number;
}

interface GridFile { meta: Meta; cells: Record<string, MadridCell>; barris: Record<string, MadridBarrio> }

export interface MadridHit { cell: MadridCell; barrio?: MadridBarrio }

// The municipality with a margin; nothing outside it needs the file.
const BOX = { south: 40.3, north: 40.65, west: -3.9, east: -3.5 };

export function nearMadrid(lat: number, lng: number): boolean {
  return lat > BOX.south && lat < BOX.north && lng > BOX.west && lng < BOX.east;
}

export function viewTouchesMadrid(b: { north: number; south: number; east: number; west: number }): boolean {
  return b.south < BOX.north && b.north > BOX.south && b.west < BOX.east && b.east > BOX.west;
}

let file: Promise<GridFile | null> | null = null;
let loaded: GridFile | null = null;

function load(): Promise<GridFile | null> {
  file ??= readData<GridFile>('madrid/grid.json').then((g) => (loaded = g));
  return file;
}

// The grid's meta once loaded (date ranges for notes), else null.
export function madridMeta(): Meta | null {
  return loaded?.meta ?? null;
}

function hit(g: GridFile, lat: number, lng: number): MadridHit | null {
  const cell = g.cells[latLngToCell(lat, lng, g.meta.h3_resolution)];
  return cell ? { cell, barrio: g.barris[String(cell.barri)] } : null;
}

export async function madridAt(lat: number, lng: number): Promise<MadridHit | null> {
  if (!nearMadrid(lat, lng)) return null;
  const g = await load();
  return g ? hit(g, lat, lng) : null;
}

// Synchronous check for the overlay, once the grid is loaded.
export function inMadridGrid(lat: number, lng: number): boolean {
  return loaded !== null && nearMadrid(lat, lng) && hit(loaded, lat, lng) !== null;
}

let polygons: { cell: MadridCell; ring: number[] }[] | null = null;

// Hex polygons for the map overlay, flattened [lat, lng, …]; built once.
export async function madridPolygons(): Promise<{ cell: MadridCell; ring: number[] }[]> {
  const g = await load();
  if (!g) return [];
  polygons ??= Object.entries(g.cells).map(([id, cell]) => ({ cell, ring: cellToBoundary(id).flat() }));
  return polygons;
}

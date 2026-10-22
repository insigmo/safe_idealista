// The user's own address: geocoded once in the popup, then used by the content
// script for the distance on each card and the "nearest first" sort.
import type { Home } from './settings';

// "41.3925, 2.1640" (or with a space instead of the comma) skips the lookup.
const LAT_LNG = /^\s*(-?\d{1,2}(?:\.\d+)?)\s*[,\s]\s*(-?\d{1,3}(?:\.\d+)?)\s*$/;

// OpenStreetMap Nominatim, limited to Spain. Called only when the user presses
// Set in the popup: one request per address, well inside its usage policy.
const NOMINATIM = 'https://nominatim.openstreetmap.org/search';

export async function geocode(query: string): Promise<Home | null> {
  const m = query.match(LAT_LNG);
  if (m) {
    const lat = Number(m[1]);
    const lng = Number(m[2]);
    if (Math.abs(lat) <= 90 && Math.abs(lng) <= 180) return { address: query.trim(), label: `${lat}, ${lng}`, lat, lng };
  }
  const url = `${NOMINATIM}?${new URLSearchParams({ q: query, format: 'jsonv2', limit: '1', countrycodes: 'es', 'accept-language': 'es' })}`;
  const r = await fetch(url, { headers: { Accept: 'application/json' } });
  if (!r.ok) throw new Error(`Nominatim: HTTP ${r.status}`);
  const hits = (await r.json()) as { lat: string; lon: string; display_name: string }[];
  if (hits.length === 0) return null;
  const { lat, lon, display_name } = hits[0];
  return { address: query.trim(), label: display_name, lat: Number(lat), lng: Number(lon) };
}

// Great-circle distance in km.
export function distanceKm(a: { lat: number; lng: number }, b: { lat: number; lng: number }): number {
  const rad = Math.PI / 180;
  const dLat = (b.lat - a.lat) * rad;
  const dLng = (b.lng - a.lng) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLng / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(h));
}

export function formatDistance(km: number): string {
  return km < 1 ? `${Math.max(50, Math.round((km * 1000) / 50) * 50)} m` : `${km < 10 ? km.toFixed(1) : Math.round(km)} km`;
}

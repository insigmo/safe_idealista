// Messages between the isolated content script and the main-world map script.
// The data is public open data, so it does not matter that the page can see it.
export const TAG = '__bcnsq';

export interface OverlayCell {
  ring: number[]; // flattened [lat, lng, …]
  color: string;
}

export interface OverlayMsg {
  [TAG]: 'overlay';
  enabled: boolean;
  opacity: number;
  title: string;
  cells: OverlayCell[];
}

// Map script -> content script, after every pan or zoom: the content script
// answers with an overlay covering the view (the Spain-wide layer is drawn
// only for what is on screen).
export interface ViewMsg {
  [TAG]: 'view';
  north: number;
  south: number;
  east: number;
  west: number;
  zoom: number;
}

export const LOG = '[BCN-SQ]';

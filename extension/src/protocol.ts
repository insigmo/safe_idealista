// Messages from the isolated content script to the main-world map script.
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

export const LOG = '[BCN-SQ]';

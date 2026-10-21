// Every Idealista-specific selector, URL pattern and markup assumption lives
// here, so a markup change means editing one file. Verified 2026-10-21
// (see NOTES.md, "Idealista integration findings").

export const SEL = {
  // One listing card in the list view. Ads use article.adv and carry no id.
  card: 'article.item[data-element-id]',
  cardIdAttr: 'data-element-id',
  // Where the badge goes: right after the title link inside the info block.
  cardInfo: '.item-info-container',
  cardTitle: 'a.item-link',
  // Google Maps container on the map view (/mapa-google).
  mapContainer: '.google-map-container',
  // Listing detail page (/inmueble/<id>/): the panel goes after the price row.
  detailPriceRow: '.detail-content-wrapper > .info-data',
  detailTitle: '.detail-content-wrapper > .main-info__title',
  // "El Raval, Barcelona": the barri, for the fallback without coordinates.
  detailLocation: '.main-info__title-minor',
} as const;

// Inline <script> that defines `config.listingMultimediaCarrousels`, whose
// per-listing "map" slide is a static-map URL with the listing coordinates.
export const CAROUSELS_KEY = 'listingMultimediaCarrousels';
// The detail page's `config.multimediaCarrousel` has the same "map" slide for
// the one listing. Matching is case-sensitive, so the list-view key never hits.
export const DETAIL_CAROUSEL_KEY = 'multimediaCarrousel:';
export const STATIC_MAP_CENTER = /[?&]center=(-?\d+(?:\.\d+)?)(?:%2C|,)(-?\d+(?:\.\d+)?)/i;
export const ADDRESS_HIDDEN = 'HIDDEN';

// Card titles look like "Piso en Calle de Mallorca, L'Antiga Esquerra de
// l'Eixample, Barcelona": the barri is the part before the city. The detail
// page's location line ("El Raval, Barcelona") has the same tail.
export function barriFromTitle(title: string): string | null {
  const parts = title.split(',').map((s) => s.trim()).filter(Boolean);
  if (parts.length < 2 || !/^barcelona$/i.test(parts[parts.length - 1])) return null;
  return parts[parts.length - 2];
}

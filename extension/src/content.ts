// Isolated-world content script: badges on listing cards, the "hide red"
// filter, the per-layer panel on a listing's detail page, and feeding hex
// polygons to the main-world map script (page.ts). Barcelona and Madrid use
// their own detailed grids; everywhere else the Spain-wide layer (spain.ts).
// It only reads what the page already rendered and the data bundled with the
// extension; it makes no network requests.
import { barriByName, barriOf, cellAt, cellPolygons, grid, level, ramp, score, type Barri, type Cell, type Level, type Values } from './grid';
import { LOG, TAG, type OverlayMsg, type ViewMsg } from './protocol';
import { ADDRESS_HIDDEN, barriFromTitle, CAROUSELS_KEY, DETAIL_CAROUSEL_KEY, SEL, STATIC_MAP_CENTER } from './selectors';
import { inMadridGrid, madridAt, madridMeta, madridPolygons, viewTouchesMadrid, type MadridHit } from './madrid';
import { CATEGORIES, CATEGORY_LABELS, loadSettings, onSettingsChanged, type Category, type Settings } from './settings';
import { cellsInView, placeAt, spainMeta, type Place } from './spain';

interface Located {
  values: Values;
  cell?: Cell;
  barri?: Barri;
  madrid?: MadridHit; // set for Madrid's grid
  place?: Place; // set for the Spain-wide layer
  approx: 'exact' | 'hidden-address' | 'barri';
}

// Layers shown for each kind of location.
const BCN_CATEGORIES = CATEGORIES.filter((c) => c !== 'income');
const MADRID_CATEGORIES: Category[] = ['dirt', 'graffiti', 'incivism', 'crime'];
const SPAIN_CATEGORIES: Category[] = ['crime', 'income'];

let settings: Settings;
const coords = new Map<string, { lat: number; lng: number; hidden: boolean }>();
let parsedScripts = new WeakSet<HTMLScriptElement>();

function safe<T extends unknown[]>(what: string, fn: (...a: T) => void): (...a: T) => void {
  return (...a: T) => {
    try {
      fn(...a);
    } catch (e) {
      console.warn(LOG, `${what} failed`, e);
    }
  };
}

// Extracts the JSON object that follows `key` in a script, honouring strings.
function objectAfter(text: string, key: string): unknown {
  const at = text.indexOf(key);
  if (at < 0) return null;
  const start = text.indexOf('{', at);
  let depth = 0;
  let inStr = false;
  for (let i = start; i < text.length; i++) {
    const ch = text[i];
    if (inStr) {
      if (ch === '\\') i++;
      else if (ch === '"') inStr = false;
    } else if (ch === '"') inStr = true;
    else if (ch === '{') depth++;
    else if (ch === '}' && --depth === 0) return JSON.parse(text.slice(start, i + 1));
  }
  return null;
}

// Reads listing coordinates from the static-map URL of each card's carousel.
function parseCarousels(): void {
  for (const s of Array.from(document.scripts)) {
    if (parsedScripts.has(s) || s.src || !s.textContent?.includes(CAROUSELS_KEY)) continue;
    parsedScripts.add(s);
    const obj = objectAfter(s.textContent, CAROUSELS_KEY) as Record<string, { map?: { src?: string; addressVisibility?: string } }> | null;
    if (!obj) continue;
    for (const [id, v] of Object.entries(obj)) {
      const m = v?.map?.src?.match(STATIC_MAP_CENTER);
      if (m) coords.set(id, { lat: Number(m[1]), lng: Number(m[2]), hidden: v.map?.addressVisibility === ADDRESS_HIDDEN });
    }
  }
}

type Coords = { lat: number; lng: number; hidden: boolean };

// Coordinates win; without them, the barri named in `title` gives its average.
// Outside Barcelona's grid the answer comes from a data file, so it is async:
// Madrid's grid near Madrid, the Spain-wide layer everywhere else.
function locateBy(c: Coords | undefined, title: string): Located | null | Promise<Located | null> {
  if (c) {
    const approx = c.hidden ? 'hidden-address' : 'exact';
    const hit = cellAt(c.lat, c.lng);
    if (hit) return { values: hit.cell, cell: hit.cell, barri: barriOf(hit.cell), approx };
    return madridAt(c.lat, c.lng).then(async (m): Promise<Located | null> => {
      if (m) return { values: m.cell, madrid: m, approx };
      const place = await placeAt(c.lat, c.lng);
      return place ? { values: place.values, place, approx } : null;
    });
  }
  const name = barriFromTitle(title);
  const b = name ? barriByName(name) : null;
  return b ? { values: b, barri: b, approx: 'barri' } : null;
}

function locate(card: HTMLElement): Located | null | Promise<Located | null> {
  const id = card.getAttribute(SEL.cardIdAttr) ?? '';
  if (!coords.has(id)) parseCarousels();
  return locateBy(coords.get(id), card.querySelector<HTMLAnchorElement>(SEL.cardTitle)?.title ?? '');
}

function locateDetail(): Located | null | Promise<Located | null> {
  let c: Coords | undefined;
  for (const s of Array.from(document.scripts)) {
    if (s.src || !s.textContent?.includes(DETAIL_CAROUSEL_KEY)) continue;
    let obj: { map?: { src?: string; addressVisibility?: string } } | null;
    try {
      obj = objectAfter(s.textContent, DETAIL_CAROUSEL_KEY) as typeof obj;
    } catch {
      continue; // the key in some other script, not followed by JSON
    }
    const m = obj?.map?.src?.match(STATIC_MAP_CENTER);
    if (m) {
      c = { lat: Number(m[1]), lng: Number(m[2]), hidden: obj?.map?.addressVisibility === ADDRESS_HIDDEN };
      break;
    }
  }
  return locateBy(c, document.querySelector(SEL.detailLocation)?.textContent ?? '');
}

function spainNotes(place: Place): string[] {
  const r = spainMeta()?.date_ranges;
  const { muni, income } = place;
  const notes = [
    muni.c1k !== undefined
      ? `Crime*: ${muni.c1k} street crimes per 1,000 residents in ${r?.crime.from.slice(0, 4) ?? 'the last full year'}, for all of ${muni.n}.`
      : `No crime figure: the Interior Ministry publishes it only for municipalities over 20,000 residents (${muni.n}: ${muni.pop.toLocaleString('en')}).`,
  ];
  if (income !== undefined) notes.push(`Income: €${income.toLocaleString('en')} net per person in ${r?.income.from ?? 'the latest year'}, this census section.`);
  return notes;
}

function madridNotes(m: MadridHit): string[] {
  const r = madridMeta()?.date_ranges;
  const notes = [
    `Citizen reports in this hex (${r?.avisos.from ?? ''} … ${r?.avisos.to ?? ''}): dirt ${m.cell.n_raw.dirt}, graffiti ${m.cell.n_raw.graffiti}. Residents ≈ ${m.cell.pop}.`,
  ];
  if (m.cell.lp) notes.push('Few residents here: values are close to the city average.');
  if (m.barrio) notes.push(`Crime* and incivism: city-police incidents in ${m.barrio.name} (${r?.police_incidents.from ?? ''} … ${r?.police_incidents.to ?? ''}): ${m.barrio.crime_n} crime, ${m.barrio.incivism_n} incivism.`);
  notes.push('Madrid publishes no smell data.');
  return notes;
}

// Notes shared by the card tooltip and the detail panel.
function locationNotes(loc: Located): string[] {
  if (loc.madrid) {
    const notes = madridNotes(loc.madrid);
    if (loc.approx === 'hidden-address') notes.push('≈ The advertiser hid the exact address; location is approximate.');
    notes.push('* Incidents handled by the city police, per barrio: not recorded crime, and the same for the whole barrio.');
    return notes;
  }
  if (loc.place) {
    const notes = spainNotes(loc.place);
    if (loc.approx === 'hidden-address') notes.push('≈ The advertiser hid the exact address; location is approximate.');
    if (loc.place.muni.c1k !== undefined) notes.push('* One crime figure per municipality: it does not tell streets apart.');
    return notes;
  }
  const notes: string[] = [];
  if (loc.cell) {
    const r = loc.cell.n_raw;
    notes.push(`Complaints in this hex (${grid.meta.date_ranges.iris.from} … ${grid.meta.date_ranges.iris.to}): dirt ${r.dirt}, smell ${r.smell}, graffiti ${r.graffiti}, incivism ${r.incivism}. Residents ≈ ${loc.cell.pop}.`);
    if (loc.cell.lp) notes.push('Few residents here: values are close to the city average.');
  }
  if (loc.approx === 'hidden-address') notes.push('≈ The advertiser hid the exact address; location is approximate.');
  if (loc.approx === 'barri') notes.push('approx: no coordinates for this listing, so this is the barri average.');
  notes.push('* Crime is official only per district; split over barris by estimate.');
  return notes;
}

// A hex can straddle a barri border, so name the area loosely.
function areaName(loc: Located): string {
  if (loc.place) return loc.place.muni.n;
  if (loc.madrid) return loc.madrid.barrio ? `${loc.madrid.barrio.name} area` : 'Madrid';
  return loc.barri ? (loc.approx === 'barri' ? loc.barri.name : `${loc.barri.name} area`) : 'Barcelona';
}

// ---- tooltip ----------------------------------------------------------

let tip: HTMLDivElement | null = null;

function showTip(badge: HTMLElement, loc: Located): void {
  tip ??= Object.assign(document.createElement('div'), { className: 'bcnsq-tip' });
  if (!tip.isConnected) document.body.append(tip);
  tip.replaceChildren();
  const head = document.createElement('div');
  head.className = 'bcnsq-tip-head';
  const s = score(loc.values, settings);
  head.textContent = `${areaName(loc)} · score ${s ?? '–'}/100`;
  tip.append(head);
  for (const c of loc.place ? SPAIN_CATEGORIES : loc.madrid ? MADRID_CATEGORIES : BCN_CATEGORIES) {
    const v = loc.values[c];
    const row = document.createElement('div');
    row.className = 'bcnsq-row' + (settings.enabled[c] && v !== undefined ? '' : ' bcnsq-off');
    const label = document.createElement('span');
    label.textContent = CATEGORY_LABELS[c] + (c === 'crime' ? '*' : '');
    const bar = document.createElement('span');
    bar.className = 'bcnsq-bar';
    const fill = document.createElement('span');
    fill.style.width = `${v ?? 0}%`;
    fill.style.background = ramp((v ?? 0) / 100);
    bar.append(fill);
    const num = document.createElement('span');
    num.textContent = v === undefined ? '–' : String(v);
    row.append(label, bar, num);
    tip.append(row);
  }
  const scope = loc.place ? 'Spain-wide' : 'city-wide';
  for (const n of [`Bars: ${scope} percentile, 0 = best, 100 = worst.`, ...locationNotes(loc)]) {
    const p = document.createElement('div');
    p.className = 'bcnsq-note';
    p.textContent = n;
    tip.append(p);
  }
  const r = badge.getBoundingClientRect();
  tip.style.display = 'block';
  const top = r.bottom + 6 + tip.offsetHeight > window.innerHeight ? r.top - tip.offsetHeight - 6 : r.bottom + 6;
  tip.style.top = `${Math.max(4, top)}px`;
  tip.style.left = `${Math.min(r.left, window.innerWidth - tip.offsetWidth - 8)}px`;
}

function hideTip(): void {
  if (tip) tip.style.display = 'none';
}

// ---- badges -------------------------------------------------------------

const located = new WeakMap<HTMLElement, Located | null>();
const locating = new WeakSet<HTMLElement>();

function renderCard(card: HTMLElement): void {
  if (!located.has(card)) {
    if (locating.has(card)) return;
    const l = locate(card);
    if (l instanceof Promise) {
      locating.add(card);
      l.catch((e) => {
        console.warn(LOG, 'Madrid/Spain-wide lookup failed', e);
        return null;
      }).then((v) => {
        located.set(card, v);
        safe('badge', renderCard)(card);
      });
      return;
    }
    located.set(card, l);
  }
  const loc = located.get(card) ?? null;
  let badge = card.querySelector<HTMLElement>(':scope .bcnsq-badge');
  const s = loc ? score(loc.values, settings) : null;
  if (!settings.badges || !loc || s === null) {
    badge?.remove();
    card.classList.remove('bcnsq-hidden');
    return;
  }
  if (!badge) {
    badge = document.createElement('span');
    badge.className = 'bcnsq-badge';
    badge.addEventListener('mouseenter', () => {
      const l = located.get(card);
      if (l) safe('tooltip', showTip)(badge!, l);
    });
    badge.addEventListener('mouseleave', hideTip);
    const title = card.querySelector(SEL.cardTitle);
    const host = card.querySelector(SEL.cardInfo);
    if (title?.parentElement) title.insertAdjacentElement('afterend', badge);
    else if (host) host.prepend(badge);
    else return;
  }
  const lv = level(s);
  badge.dataset.level = lv;
  const mark = loc.approx === 'hidden-address' ? ' ≈' : loc.approx === 'barri' ? ' approx' : '';
  // Barcelona's grid scores the street; the Spain-wide layer only the area.
  const kind = loc.place ? 'Area' : 'Street';
  const text = `${kind} ${s}${mark}`;
  // Only touch the DOM on change: our own writes would re-trigger the observer.
  if (badge.textContent !== text) badge.textContent = text;
  badge.setAttribute('aria-label', `${kind} quality score ${s} of 100${mark ? ', approximate' : ''}`);
  card.classList.toggle('bcnsq-hidden', settings.hideRed && lv === 'bad');
}

// ---- detail page panel --------------------------------------------------------

// Safety is crime read the other way round, so that its word and bar say how
// safe the area is rather than how much crime it has.
const PANEL_ROWS: { c: Category; label: string; inverse?: boolean; minor?: boolean }[] = [
  { c: 'dirt', label: 'Dirt' },
  { c: 'graffiti', label: 'Graffiti' },
  { c: 'crime', label: 'Safety*', inverse: true },
  { c: 'smell', label: 'Smell', minor: true },
  { c: 'incivism', label: 'Incivism', minor: true },
];
const PROBLEM_WORD: Record<Level, string> = { good: 'Low', mid: 'Medium', bad: 'High' };
const SAFETY_WORD: Record<Level, string> = { good: 'High', mid: 'Medium', bad: 'Low' };
const LEVEL_WORD: Record<Level, string> = { good: 'Good', mid: 'Average', bad: 'Poor' };
const VERDICT: Record<Level, string> = {
  good: 'Cleaner and safer than most of Barcelona.',
  mid: 'Around the city average: worth walking the street before deciding.',
  bad: 'Dirtier or less safe than most of Barcelona.',
};
// Outside Barcelona: municipal crime and section income only. Income reads
// the other way round too, so its bar shows how well-off the area is.
const SPAIN_ROWS: typeof PANEL_ROWS = [
  { c: 'crime', label: 'Safety*', inverse: true },
  { c: 'income', label: 'Income', inverse: true },
];
// Madrid has no smell layer.
const MADRID_ROWS = PANEL_ROWS.filter((r) => r.c !== 'smell');
const MADRID_VERDICT: Record<Level, string> = {
  good: 'Cleaner and safer than most of Madrid.',
  mid: 'Around the city average: worth walking the street before deciding.',
  bad: 'Dirtier or less safe than most of Madrid.',
};
const SPAIN_VERDICT: Record<Level, string> = {
  good: 'Better than most of Spain on the layers shown.',
  mid: 'Around the Spanish average: worth walking the street before deciding.',
  bad: 'Worse than most of Spain on the layers shown.',
};

let detailLoc: Located | null | undefined | 'pending';

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  if (text !== undefined) e.textContent = text;
  return e;
}

function renderDetail(): void {
  const anchor = document.querySelector(SEL.detailPriceRow) ?? document.querySelector(SEL.detailTitle);
  if (!anchor) return; // not a detail page
  if (detailLoc === undefined) {
    const l = locateDetail();
    if (l instanceof Promise) {
      detailLoc = 'pending';
      l.catch((e) => {
        console.warn(LOG, 'Madrid/Spain-wide lookup failed', e);
        return null;
      }).then((v) => {
        detailLoc = v;
        safe('panel', renderDetail)();
      });
      return;
    }
    detailLoc = l;
  }
  if (detailLoc === 'pending') return;
  const loc = detailLoc;
  const old = document.querySelector<HTMLElement>('.bcnsq-panel');
  if (!settings.badges || !loc) {
    old?.remove();
    return;
  }
  const s = score(loc.values, settings);
  // Only touch the DOM on change: our own writes would re-trigger the observer.
  const key = JSON.stringify([s, settings.enabled]);
  if (old?.dataset.key === key) return;

  const panel = el('section', 'bcnsq-panel');
  panel.dataset.key = key;
  panel.setAttribute('aria-label', loc.place ? 'Area quality' : 'Street quality');
  const head = el('div', 'bcnsq-panel-head');
  const lv = s === null ? null : level(s);
  const big = el('span', 'bcnsq-score', s === null ? '–' : String(s));
  if (lv) big.dataset.level = lv;
  const titles = el('div', '');
  titles.append(
    el('div', 'bcnsq-panel-title', `${loc.place ? 'Area' : 'Street'} quality: ${lv ? LEVEL_WORD[lv] : 'all layers are off'}`),
    el('div', 'bcnsq-panel-sub', `${areaName(loc)}${loc.approx === 'exact' ? '' : loc.approx === 'barri' ? ' · barri average' : ' · approximate location'} · score out of 100`),
  );
  head.append(big, titles);
  panel.append(head);
  if (lv) panel.append(el('p', 'bcnsq-verdict', (loc.place ? SPAIN_VERDICT : loc.madrid ? MADRID_VERDICT : VERDICT)[lv]));

  for (const { c, label, inverse, minor } of loc.place ? SPAIN_ROWS : loc.madrid ? MADRID_ROWS : PANEL_ROWS) {
    const p = loc.values[c];
    if (p === undefined) {
      const row = el('div', 'bcnsq-prow bcnsq-off');
      row.title = 'No data for this place.';
      row.append(el('span', '', label), el('span', 'bcnsq-bar'), el('span', 'bcnsq-chip bcnsq-nodata', 'No data'), el('span', 'bcnsq-num', '–'));
      panel.append(row);
      continue;
    }
    const rowLevel = level(100 - p);
    const shown = inverse ? 100 - p : p;
    const row = el('div', 'bcnsq-prow' + (minor ? ' bcnsq-minor' : '') + (settings.enabled[c] ? '' : ' bcnsq-off'));
    if (!settings.enabled[c]) row.title = 'Switched off in the extension settings: not counted in the score.';
    const bar = el('span', 'bcnsq-bar');
    const fill = el('span', '');
    fill.style.width = `${shown}%`;
    fill.style.background = ramp(p / 100);
    bar.append(fill);
    const chip = el('span', 'bcnsq-chip', (inverse ? SAFETY_WORD : PROBLEM_WORD)[rowLevel]);
    chip.dataset.level = rowLevel;
    row.append(el('span', '', label), bar, chip, el('span', 'bcnsq-num', String(shown)));
    panel.append(row);
  }

  const notes = [
    loc.place
      ? 'Safety and income: Spain-wide percentile, 100 = safest / highest income. Score: 100 = best. Same data as the map overlay.'
      : `Dirt, graffiti, ${loc.madrid ? '' : 'smell, '}incivism: city-wide percentile, 0 = least, 100 = most. Safety and score: 100 = best. Same data as the map overlay.`,
    ...locationNotes(loc),
  ];
  for (const n of notes) panel.append(el('div', 'bcnsq-note', n));

  if (old) old.replaceWith(panel);
  else anchor.insertAdjacentElement('afterend', panel);
}

function renderAll(): void {
  document.querySelectorAll<HTMLElement>(SEL.card).forEach(safe('badge', renderCard));
  safe('panel', renderDetail)();
}

// ---- map overlay ------------------------------------------------------------

function overlayTitle(): string {
  const on = CATEGORIES.filter((c) => settings.enabled[c] && settings.weights[c] > 0);
  if (on.length === 1) return CATEGORY_LABELS[on[0]];
  return on.length === CATEGORIES.length ? 'score' : `score (${on.map((c) => CATEGORY_LABELS[c]).join(', ')})`;
}

// Hexes that depend on the map view: Madrid's grid once the view reaches
// Madrid (then kept), and the Spain-wide layer for what is on screen.
// Barcelona's grid is sent whole.
let view: ViewMsg | null = null;
let madridCells: Awaited<ReturnType<typeof madridPolygons>> = [];
let spainCells: { ring: number[]; values: Values }[] = [];
let viewSeq = 0;

async function refreshViewCells(): Promise<void> {
  const seq = ++viewSeq;
  const v = view;
  if (v && viewTouchesMadrid(v) && madridCells.length === 0) madridCells = await madridPolygons();
  const skip = (lat: number, lng: number) => cellAt(lat, lng) !== null || inMadridGrid(lat, lng);
  const cells = v ? await cellsInView(v, v.zoom, skip) : [];
  if (seq !== viewSeq) return; // a newer view arrived meanwhile
  spainCells = cells;
  sendOverlay();
}

function sendOverlay(): void {
  const cells = [];
  for (const { cell, ring } of cellPolygons) {
    if (settings.hideLowPop && cell.lp) continue;
    const s = score(cell, settings);
    if (s === null) continue;
    cells.push({ ring, color: ramp(1 - s / 100) });
  }
  for (const { cell, ring } of madridCells) {
    if (settings.hideLowPop && cell.lp) continue;
    const s = score(cell, settings);
    if (s !== null) cells.push({ ring, color: ramp(1 - s / 100) });
  }
  for (const { ring, values } of spainCells) {
    const s = score(values, settings);
    if (s !== null) cells.push({ ring, color: ramp(1 - s / 100) });
  }
  const msg: OverlayMsg = { [TAG]: 'overlay', enabled: settings.overlay, opacity: settings.opacity, title: overlayTitle(), cells };
  window.postMessage(msg, window.location.origin);
}

// ---- wiring -----------------------------------------------------------------

async function main(): Promise<void> {
  settings = await loadSettings();
  renderAll();
  sendOverlay();
  let pending = false;
  new MutationObserver(() => {
    if (pending) return;
    pending = true;
    requestAnimationFrame(() => {
      pending = false;
      safe('render', renderAll)();
    });
  }).observe(document.body, { childList: true, subtree: true });
  onSettingsChanged((s) => {
    settings = s;
    safe('render', renderAll)();
    safe('overlay', sendOverlay)();
  });
  window.addEventListener('scroll', hideTip, { passive: true });
  window.addEventListener('message', (e: MessageEvent) => {
    if (e.source !== window || e.data?.[TAG] !== 'view') return;
    view = e.data as ViewMsg;
    refreshViewCells().catch((err) => console.warn(LOG, 'Madrid/Spain-wide overlay failed', err));
  });
  // Idealista swaps the result list in place on some filter changes; new
  // inline config scripts are picked up because parsed scripts are tracked.
  window.addEventListener('pageshow', () => {
    parsedScripts = new WeakSet();
  });
  console.info(LOG, `ready: ${coords.size} listing coordinates, grid ${grid.meta.generated_at}`);
}

main().catch((e) => console.warn(LOG, 'init failed', e));

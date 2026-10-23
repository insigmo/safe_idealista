// Isolated-world content script: badges on listing cards, the "hide red"
// filter, the distance to the user's address and the "nearest first" sort on
// list pages, the per-layer panel on a listing's detail page, and feeding hex
// polygons to the main-world map script (page.ts). Barcelona and Madrid use
// their own detailed grids; everywhere else the Spain-wide layer (spain.ts).
// It only reads what the page already rendered and the data bundled with the
// extension; it makes no network requests.
import { barriByName, barriOf, cellAt, cellPolygons, grid, level, loadGrid, ramp, score, type Barri, type Cell, type Values } from './grid';
import { LOG, TAG, type OverlayMsg, type ViewMsg } from './protocol';
import { ADDRESS_HIDDEN, barriFromTitle, CAROUSELS_KEY, DETAIL_CAROUSEL_KEY, SEL, STATIC_MAP_CENTER } from './selectors';
import { inMadridGrid, madridAt, madridMeta, madridPolygons, viewTouchesMadrid, type MadridHit } from './madrid';
import { distanceKm, formatDistance } from './home';
import { pickLang, strings, type Strings } from './i18n';
import { CATEGORIES, effective, loadSettings, onSettingsChanged, type Category, type Lang, type Settings } from './settings';
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
// On-page text follows the language picked in the popup.
let lang: Lang;
let t: Strings;
let p: Strings['page'];
const num = (n: number): string => n.toLocaleString(lang);

function applySettings(s: Settings): void {
  settings = effective(s);
  lang = pickLang(s.lang);
  t = strings(lang);
  p = t.page;
}
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

function cardCoords(card: HTMLElement): Coords | undefined {
  const id = card.getAttribute(SEL.cardIdAttr) ?? '';
  if (!coords.has(id)) parseCarousels();
  return coords.get(id);
}

function locate(card: HTMLElement): Located | null | Promise<Located | null> {
  return locateBy(cardCoords(card), card.querySelector<HTMLAnchorElement>(SEL.cardTitle)?.title ?? '');
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
      ? p.spainCrime(num(muni.c1k), r?.crime.from.slice(0, 4) ?? '?', muni.n)
      : p.spainNoCrime(muni.n, num(muni.pop)),
  ];
  if (income !== undefined) notes.push(p.spainIncome(num(income), r?.income.from ?? '?'));
  return notes;
}

function madridNotes(m: MadridHit): string[] {
  const r = madridMeta()?.date_ranges;
  const notes = [p.madridReports(r?.avisos.from ?? '', r?.avisos.to ?? '', num(m.cell.n_raw.dirt), num(m.cell.n_raw.graffiti), num(m.cell.pop))];
  if (m.cell.lp) notes.push(p.fewPeople);
  if (m.barrio) notes.push(p.madridPolice(m.barrio.name, r?.police_incidents.from ?? '', r?.police_incidents.to ?? '', num(m.barrio.crime_n), num(m.barrio.incivism_n)));
  notes.push(p.madridNoSmell);
  return notes;
}

// Notes shared by the card tooltip and the detail panel.
function locationNotes(loc: Located): string[] {
  if (loc.madrid) {
    const notes = madridNotes(loc.madrid);
    if (loc.approx === 'hidden-address') notes.push(p.hiddenAddress);
    notes.push(p.madridCrimeNote);
    return notes;
  }
  if (loc.place) {
    const notes = spainNotes(loc.place);
    if (loc.approx === 'hidden-address') notes.push(p.hiddenAddress);
    if (loc.place.muni.c1k !== undefined) notes.push(p.spainCrimeNote);
    return notes;
  }
  const notes: string[] = [];
  if (loc.cell) {
    const r = loc.cell.n_raw;
    const { from, to } = grid.meta.date_ranges.iris;
    notes.push(p.bcnComplaints(from, to, num(r.dirt), num(r.smell), num(r.graffiti), num(r.incivism), num(loc.cell.pop)));
    if (loc.cell.lp) notes.push(p.fewPeople);
  }
  if (loc.approx === 'hidden-address') notes.push(p.hiddenAddress);
  if (loc.approx === 'barri') notes.push(p.barriAverage);
  notes.push(p.bcnCrimeNote);
  return notes;
}

// A hex can straddle a barri border, so name the area loosely.
function areaName(loc: Located): string {
  if (loc.place) return loc.place.muni.n;
  if (loc.madrid) return loc.madrid.barrio ? p.near(loc.madrid.barrio.name) : 'Madrid';
  return loc.barri ? (loc.approx === 'barri' ? loc.barri.name : p.near(loc.barri.name)) : 'Barcelona';
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
  head.textContent = p.scoreHead(areaName(loc), s === null ? '–' : String(s));
  tip.append(head);
  for (const c of loc.place ? SPAIN_CATEGORIES : loc.madrid ? MADRID_CATEGORIES : BCN_CATEGORIES) {
    const v = loc.values[c];
    const row = document.createElement('div');
    row.className = 'bcnsq-row' + (settings.enabled[c] && v !== undefined ? '' : ' bcnsq-off');
    const label = document.createElement('span');
    label.textContent = t.cats[c] + (c === 'crime' ? '*' : '');
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
  for (const n of [p.bars(!loc.place), ...locationNotes(loc)]) {
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
  const mark = loc.approx === 'hidden-address' ? ' ≈' : loc.approx === 'barri' ? ` ${p.approx}` : '';
  // Barcelona's grid scores the street; the Spain-wide layer only the area.
  const kind = loc.place ? p.area : p.street;
  const text = `${kind} ${s}${mark}`;
  // Only touch the DOM on change: our own writes would re-trigger the observer.
  if (badge.textContent !== text) badge.textContent = text;
  const aria = p.badgeLabel(kind, s, mark !== '');
  if (badge.getAttribute('aria-label') !== aria) badge.setAttribute('aria-label', aria);
  card.classList.toggle('bcnsq-hidden', settings.hideRed && s < settings.hideBelow);
}

// ---- distance to the user's address, "nearest first" sort -------------------

// Idealista sorts on the server and has no distance order, so this one sorts
// the cards already on the page. It is per tab and survives pagination;
// picking any of Idealista's own orders switches it off.
const SORT_KEY = 'bcnsq-sort';

function sortByDistance(): boolean {
  if (!settings.home) return false;
  try {
    return sessionStorage.getItem(SORT_KEY) === 'distance';
  } catch {
    return false;
  }
}

function setSortByDistance(on: boolean): void {
  try {
    if (on) sessionStorage.setItem(SORT_KEY, 'distance');
    else sessionStorage.removeItem(SORT_KEY);
  } catch {
    // storage blocked: the sort just does not persist
  }
}

function renderDistance(card: HTMLElement): void {
  let chip = card.querySelector<HTMLElement>(':scope .bcnsq-dist');
  const home = settings.home;
  const c = home ? cardCoords(card) : undefined;
  if (!home || !c) {
    chip?.remove();
    return;
  }
  if (!chip) {
    const title = card.querySelector(SEL.cardTitle);
    if (!title?.parentElement) return;
    chip = el('span', 'bcnsq-dist');
    (card.querySelector(':scope .bcnsq-badge') ?? title).insertAdjacentElement('afterend', chip);
  }
  const text = `${c.hidden ? '≈ ' : ''}${formatDistance(distanceKm(home, c))}`;
  const hint = p.distHint(home.label, c.hidden);
  // Only touch the DOM on change: our own writes would re-trigger the observer.
  if (chip.textContent !== text) chip.textContent = text;
  if (chip.title !== hint) chip.title = hint;
}

// Order in which cards were first seen, to undo the sort.
const firstSeen = new WeakMap<HTMLElement, number>();
let seenCount = 0;

// Reorders the cards inside their list; ads and other blocks keep their slots.
// Cards without coordinates go last.
function sortCards(): void {
  const home = sortByDistance() ? settings.home : null;
  const lists = new Map<Element, HTMLElement[]>();
  for (const card of Array.from(document.querySelectorAll<HTMLElement>(SEL.card))) {
    if (!firstSeen.has(card)) firstSeen.set(card, seenCount++);
    const list = card.parentElement;
    if (!list) continue;
    if (!lists.has(list)) lists.set(list, []);
    lists.get(list)!.push(card);
  }
  for (const cards of lists.values()) {
    const keyed = cards.map((card) => {
      const c = home ? cardCoords(card) : undefined;
      return { card, d: !home ? 0 : c ? distanceKm(home, c) : Infinity, i: firstSeen.get(card)! };
    });
    keyed.sort((a, b) => (a.d === b.d ? a.i - b.i : a.d - b.d));
    if (keyed.every((k, i) => k.card === cards[i])) continue;
    const slots = cards.map((card) => {
      const mark = document.createComment('');
      card.before(mark);
      return mark;
    });
    keyed.forEach((k, i) => slots[i].replaceWith(k.card));
  }
}

// Adds the option to Idealista's "Ordenar" dropdown and mirrors the selected
// state (the drop button shows the active order).
function renderSortOption(): void {
  const root = document.querySelector<HTMLElement>(SEL.orderBy);
  const menu = document.querySelector(SEL.orderByDropdown);
  if (!root || !menu) return;
  if (!root.dataset.bcnsq) {
    root.dataset.bcnsq = '1';
    // Any of Idealista's own options ends ours. Relevancia is a plain span
    // that does not reload the page, so re-render to restore the order.
    root.addEventListener('click', (e) => {
      if (!(e.target as Element).closest(SEL.orderByOption) || !sortByDistance()) return;
      setSortByDistance(false);
      queueMicrotask(safe('render', renderAll));
    }, true);
  }
  let item = menu.querySelector<HTMLElement>(':scope > li.bcnsq-sort');
  if (!settings.home) {
    item?.remove();
  } else if (!item) {
    item = el('li', 'bcnsq-sort');
    const opt = el('span', 'bcnsq-sort-opt', p.sortLabel);
    opt.tabIndex = 0;
    opt.setAttribute('role', 'button');
    const choose = (e: Event) => {
      e.preventDefault();
      e.stopPropagation();
      setSortByDistance(true);
      (document.activeElement as HTMLElement | null)?.blur();
      safe('render', renderAll)();
    };
    opt.addEventListener('click', choose);
    opt.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') choose(e);
    });
    item.append(opt);
    menu.prepend(item);
  }
  const on = sortByDistance();
  const opt = item?.firstElementChild;
  if (opt && opt.textContent !== p.sortLabel) opt.textContent = p.sortLabel;
  if (opt && opt.classList.contains('selected') !== on) opt.classList.toggle('selected', on);
  for (const e of Array.from(root.querySelectorAll<HTMLElement>(`${SEL.orderByOption}.selected, [data-bcnsq-selected]`))) {
    if (on && e.classList.contains('selected')) {
      e.dataset.bcnsqSelected = '1';
      e.classList.remove('selected');
    } else if (!on && e.dataset.bcnsqSelected) {
      delete e.dataset.bcnsqSelected;
      e.classList.add('selected');
    }
  }
  const drop = root.querySelector<HTMLElement>(SEL.orderByDropButton);
  if (drop) {
    drop.dataset.bcnsqText ??= drop.textContent?.trim() ?? '';
    const text = on ? p.sortLabel : drop.dataset.bcnsqText;
    if (drop.textContent?.trim() !== text) drop.textContent = text;
  }
}

// ---- detail page panel --------------------------------------------------------

// Every row reads the same way: a longer bar is worse. Crime shows as
// unsafety and income as low income, the way the data has them.
const PANEL_ROWS: { c: Category; minor?: boolean }[] = [
  { c: 'dirt' },
  { c: 'graffiti' },
  { c: 'crime' },
  { c: 'smell', minor: true },
  { c: 'incivism', minor: true },
];
// Outside Barcelona and Madrid: municipal crime and section income only.
const SPAIN_ROWS: typeof PANEL_ROWS = [{ c: 'crime' }, { c: 'income' }];
// Madrid has no smell layer.
const MADRID_ROWS = PANEL_ROWS.filter((r) => r.c !== 'smell');

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
  const key = JSON.stringify([s, settings.enabled, lang]);
  if (old?.dataset.key === key) return;

  const panel = el('section', 'bcnsq-panel');
  panel.dataset.key = key;
  const lv = s === null ? null : level(s);
  panel.setAttribute('aria-label', p.panelTitle(!!loc.place, lv ? p.levelWord[lv] : p.allOff));
  const head = el('div', 'bcnsq-panel-head');
  const big = el('span', 'bcnsq-score', s === null ? '–' : String(s));
  if (lv) big.dataset.level = lv;
  const titles = el('div', '');
  titles.append(
    el('div', 'bcnsq-panel-title', p.panelTitle(!!loc.place, lv ? p.levelWord[lv] : p.allOff)),
    el('div', 'bcnsq-panel-sub', `${areaName(loc)}${loc.approx === 'exact' ? '' : loc.approx === 'barri' ? p.subBarri : p.subApprox}${p.subOutOf}`),
  );
  head.append(big, titles);
  panel.append(head);
  if (lv) panel.append(el('p', 'bcnsq-verdict', p.verdict[loc.place ? 'spain' : loc.madrid ? 'madrid' : 'bcn'][lv]));

  for (const { c, minor } of loc.place ? SPAIN_ROWS : loc.madrid ? MADRID_ROWS : PANEL_ROWS) {
    const label = t.cats[c] + (c === 'crime' ? '*' : '');
    const v = loc.values[c];
    if (v === undefined) {
      const row = el('div', 'bcnsq-prow bcnsq-off');
      row.title = p.noDataTitle;
      row.append(el('span', '', label), el('span', 'bcnsq-bar'), el('span', 'bcnsq-chip bcnsq-nodata', p.noData), el('span', 'bcnsq-num', '–'));
      panel.append(row);
      continue;
    }
    const rowLevel = level(100 - v);
    const row = el('div', 'bcnsq-prow' + (minor ? ' bcnsq-minor' : '') + (settings.enabled[c] ? '' : ' bcnsq-off'));
    if (!settings.enabled[c]) row.title = p.offTitle;
    const bar = el('span', 'bcnsq-bar');
    const fill = el('span', '');
    fill.style.width = `${v}%`;
    fill.style.background = ramp(v / 100);
    bar.append(fill);
    const chip = el('span', 'bcnsq-chip', p.amount[rowLevel]);
    chip.dataset.level = rowLevel;
    row.append(el('span', '', label), bar, chip, el('span', 'bcnsq-num', String(v)));
    panel.append(row);
  }

  const notes = [p.panelNote(!loc.place), ...locationNotes(loc)];
  for (const n of notes) panel.append(el('div', 'bcnsq-note', n));

  if (old) old.replaceWith(panel);
  else anchor.insertAdjacentElement('afterend', panel);
}

function renderAll(): void {
  document.querySelectorAll<HTMLElement>(SEL.card).forEach(safe('badge', renderCard));
  document.querySelectorAll<HTMLElement>(SEL.card).forEach(safe('distance', renderDistance));
  safe('sort option', renderSortOption)();
  safe('sort', sortCards)();
  safe('panel', renderDetail)();
}

// ---- map overlay ------------------------------------------------------------

function overlayTitle(): string {
  const on = CATEGORIES.filter((c) => settings.enabled[c] && settings.weights[c] > 0);
  if (on.length === 1) return t.cats[on[0]];
  return on.length === CATEGORIES.length ? p.mapScore : `${p.mapScore} (${on.map((c) => t.cats[c]).join(', ')})`;
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
  for (const { cell, ring } of cellPolygons()) {
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
  const msg: OverlayMsg = { [TAG]: 'overlay', enabled: settings.overlay, opacity: settings.opacity, title: overlayTitle(), better: p.mapBetter, worse: p.mapWorse, cells };
  window.postMessage(msg, window.location.origin);
}

// ---- wiring -----------------------------------------------------------------

async function main(): Promise<void> {
  const [stored] = await Promise.all([loadSettings(), loadGrid()]);
  applySettings(stored);
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
    applySettings(s);
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

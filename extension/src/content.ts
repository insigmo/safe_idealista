// Isolated-world content script: badges on listing cards, the "hide red"
// filter, and feeding hex polygons to the main-world map script (page.ts).
// It only reads what the page already rendered; it makes no network requests.
import { barriByName, barriOf, cellAt, cellPolygons, grid, level, ramp, score, type Barri, type Cell, type Values } from './grid';
import { LOG, TAG, type OverlayMsg } from './protocol';
import { ADDRESS_HIDDEN, barriFromTitle, CAROUSELS_KEY, SEL, STATIC_MAP_CENTER } from './selectors';
import { CATEGORIES, CATEGORY_LABELS, loadSettings, onSettingsChanged, type Settings } from './settings';

interface Located {
  values: Values;
  cell?: Cell;
  barri?: Barri;
  approx: 'exact' | 'hidden-address' | 'barri';
}

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

function locate(card: HTMLElement): Located | null {
  const id = card.getAttribute(SEL.cardIdAttr) ?? '';
  if (!coords.has(id)) parseCarousels();
  const c = coords.get(id);
  if (c) {
    const hit = cellAt(c.lat, c.lng);
    if (hit) return { values: hit.cell, cell: hit.cell, barri: barriOf(hit.cell), approx: c.hidden ? 'hidden-address' : 'exact' };
    return null; // outside Barcelona's grid
  }
  const title = card.querySelector<HTMLAnchorElement>(SEL.cardTitle)?.title ?? '';
  const name = barriFromTitle(title);
  const b = name ? barriByName(name) : null;
  return b ? { values: b, barri: b, approx: 'barri' } : null;
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
  // A hex can straddle a barri border, so name the area loosely.
  const area = loc.barri ? (loc.approx === 'barri' ? loc.barri.name : `${loc.barri.name} area`) : 'Barcelona';
  head.textContent = `${area} · score ${s ?? '–'}/100`;
  tip.append(head);
  for (const c of CATEGORIES) {
    const row = document.createElement('div');
    row.className = 'bcnsq-row' + (settings.enabled[c] ? '' : ' bcnsq-off');
    const label = document.createElement('span');
    label.textContent = CATEGORY_LABELS[c] + (c === 'crime' ? '*' : '');
    const bar = document.createElement('span');
    bar.className = 'bcnsq-bar';
    const fill = document.createElement('span');
    fill.style.width = `${loc.values[c]}%`;
    fill.style.background = ramp(loc.values[c] / 100);
    bar.append(fill);
    const num = document.createElement('span');
    num.textContent = String(loc.values[c]);
    row.append(label, bar, num);
    tip.append(row);
  }
  const notes: string[] = ['Bars: city-wide percentile, 0 = best, 100 = worst.'];
  if (loc.cell) {
    const r = loc.cell.n_raw;
    notes.push(`Complaints in this hex (${grid.meta.date_ranges.iris.from} … ${grid.meta.date_ranges.iris.to}): dirt ${r.dirt}, smell ${r.smell}, graffiti ${r.graffiti}, incivism ${r.incivism}. Residents ≈ ${loc.cell.pop}.`);
    if (loc.cell.lp) notes.push('Few residents here: values are close to the city average.');
  }
  if (loc.approx === 'hidden-address') notes.push('≈ The advertiser hid the exact address; location is approximate.');
  if (loc.approx === 'barri') notes.push('approx: no coordinates on this card, so this is the barri average.');
  notes.push('* Crime is official only per district; split over barris by estimate.');
  for (const n of notes) {
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

function renderCard(card: HTMLElement): void {
  if (!located.has(card)) located.set(card, locate(card));
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
  const text = `Street ${s}${mark}`;
  // Only touch the DOM on change: our own writes would re-trigger the observer.
  if (badge.textContent !== text) badge.textContent = text;
  badge.setAttribute('aria-label', `BCN street quality score ${s} of 100${mark ? ', approximate' : ''}`);
  card.classList.toggle('bcnsq-hidden', settings.hideRed && lv === 'bad');
}

function renderAll(): void {
  document.querySelectorAll<HTMLElement>(SEL.card).forEach(safe('badge', renderCard));
}

// ---- map overlay ------------------------------------------------------------

function overlayTitle(): string {
  const on = CATEGORIES.filter((c) => settings.enabled[c] && settings.weights[c] > 0);
  if (on.length === 1) return CATEGORY_LABELS[on[0]];
  return on.length === CATEGORIES.length ? 'score' : `score (${on.map((c) => CATEGORY_LABELS[c]).join(', ')})`;
}

function sendOverlay(): void {
  const cells = [];
  for (const { cell, ring } of cellPolygons) {
    if (settings.hideLowPop && cell.lp) continue;
    const s = score(cell, settings);
    if (s === null) continue;
    cells.push({ ring, color: ramp(1 - s / 100) });
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
  // Idealista swaps the result list in place on some filter changes; new
  // inline config scripts are picked up because parsed scripts are tracked.
  window.addEventListener('pageshow', () => {
    parsedScripts = new WeakSet();
  });
  console.info(LOG, `ready: ${coords.size} listing coordinates, grid ${grid.meta.generated_at}`);
}

main().catch((e) => console.warn(LOG, 'init failed', e));

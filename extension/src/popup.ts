import { dataStatus, readData, REFRESH_MSG } from './data';
import type { Meta } from './grid';
import { geocode } from './home';
import { LANG_NAMES, pickLang, strings, type Strings } from './i18n';
import { CATEGORIES, DEFAULTS, LANGS, loadSettings, saveSettings, type Lang, type Settings } from './settings';

const $ = <T extends HTMLElement>(id: string): T => document.getElementById(id) as T;

let settings: Settings;
let t: Strings;
let data: { bcn: Meta | null; madrid: Meta | null; spain: Meta | null } = { bcn: null, madrid: null, spain: null };

// Publisher homepages for the sources dialog, keyed by the portal name the
// pipeline writes into meta.json. note = licence or attribution, where known.
const PORTALS: Record<string, { home: string; note?: string }> = {
  'Open Data BCN': { home: 'https://opendata-ajuntament.barcelona.cat', note: 'CC BY 4.0' },
  'Dades obertes de Catalunya (Socrata)': { home: 'https://analisi.transparenciacatalunya.cat' },
  'Datos abiertos Ayuntamiento de Madrid': { home: 'https://datos.madrid.es' },
  'Geoportal Madrid': { home: 'https://geoportal.madrid.es' },
  INE: { home: 'https://www.ine.es' },
  'Ministerio del Interior — Portal Estadístico de Criminalidad': { home: 'https://estadisticasdecriminalidad.ses.mir.es' },
};

function save(): void {
  void saveSettings(settings);
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, props: Partial<HTMLElementTagNameMap[K]> = {}): HTMLElementTagNameMap[K] {
  return Object.assign(document.createElement(tag), props);
}

function applyLang(): void {
  const lang = pickLang(settings.lang);
  t = strings(lang);
  document.documentElement.lang = lang;
  for (const e of document.querySelectorAll<HTMLElement>('[data-i18n]')) e.textContent = t[e.dataset.i18n as keyof Strings] as string;
  for (const e of document.querySelectorAll<HTMLElement>('[data-i18n-label]')) e.setAttribute('aria-label', t[e.dataset.i18nLabel as keyof Strings] as string);
  for (const e of document.querySelectorAll<HTMLElement>('[data-i18n-title]')) e.title = t[e.dataset.i18nTitle as keyof Strings] as string;
  $('caveats').replaceChildren(...t.caveatList.map((c) => el('li', { textContent: c })));
  renderPower();
  renderControls();
  renderHome();
  $('autoLang').textContent = t.auto;
  renderMeta();
  renderSources();
  void renderStatus();
}

function renderPower(): void {
  $<HTMLInputElement>('on').checked = settings.on;
  $('onText').textContent = settings.on ? t.on : t.off;
  $('paused').hidden = settings.on;
  document.body.classList.toggle('off', !settings.on);
}

function renderControls(): void {
  for (const key of ['badges', 'overlay', 'hideRed', 'hideLowPop'] as const) {
    $<HTMLInputElement>(key).checked = settings[key];
  }
  const below = $<HTMLInputElement>('hideBelow');
  below.value = String(settings.hideBelow);
  below.disabled = !settings.hideRed;
  $<HTMLInputElement>('opacity').value = String(settings.opacity);
  $<HTMLOutputElement>('opacityOut').value = `${Math.round(settings.opacity * 100)}%`;

  const cats = $('cats');
  cats.replaceChildren();
  for (const c of CATEGORIES) {
    const label = el('label');
    const box = el('input', { type: 'checkbox', checked: settings.enabled[c] });
    label.append(box, t.cats[c]);
    const slider = el('input', { type: 'range', min: '0', max: '100', step: '5', value: String(settings.weights[c]), disabled: !settings.enabled[c] });
    slider.setAttribute('aria-label', t.cats[c]);
    const out = el('output', { value: String(settings.weights[c]) });
    box.addEventListener('change', () => {
      settings.enabled[c] = box.checked;
      slider.disabled = !box.checked;
      save();
    });
    slider.addEventListener('input', () => {
      settings.weights[c] = Number(slider.value);
      out.value = slider.value;
      save();
    });
    const row = el('div', { className: 'cat' });
    row.append(label, slider, out);
    cats.append(row);
  }
}

function renderHome(): void {
  $('homeError').hidden = true;
  $('homeFound').hidden = !settings.home;
  $('homeStatus').textContent = settings.home ? t.found + settings.home.label : '';
  $('homeStatus').title = settings.home?.label ?? '';
  $<HTMLInputElement>('homeInput').value = settings.home?.address ?? '';
}

function homeError(text: string): void {
  $('homeError').textContent = text;
  $('homeError').hidden = false;
}

async function setHome(e: SubmitEvent): Promise<void> {
  e.preventDefault();
  const query = $<HTMLInputElement>('homeInput').value.trim();
  if (!query) return;
  const button = $<HTMLButtonElement>('homeSet');
  button.disabled = true;
  button.textContent = t.looking;
  $('homeError').hidden = true;
  try {
    const home = await geocode(query);
    if (!home) {
      homeError(t.notFound);
      return;
    }
    settings.home = home;
    save();
    renderHome();
  } catch (err) {
    console.warn('address lookup failed', err);
    homeError(t.lookupFailed);
  } finally {
    button.disabled = false;
    button.textContent = t.set;
  }
}

function renderMeta(): void {
  const { bcn: meta, madrid, spain } = data;
  if (!meta) return;
  const r = meta.date_ranges;
  // Source names stay as published, so these labels need no translation.
  const rows: [string, string][] = [
    ['Barcelona · IRIS', `${r.iris.from} … ${r.iris.to}`],
    ['Barcelona · Mossos', `${r.crime.from} … ${r.crime.to}`],
    ['Barcelona · Guàrdia Urbana', `${r.gub_incidents.from} … ${r.gub_incidents.to}`],
    ['Barcelona · Padró', r.population.from],
  ];
  if (madrid) {
    const m = madrid.date_ranges;
    rows.push(['Madrid · Avisos', `${m.avisos.from} … ${m.avisos.to}`], ['Madrid · Policía Municipal', `${m.police_incidents.from} … ${m.police_incidents.to}`]);
  }
  if (spain) {
    const e = spain.date_ranges;
    rows.push(['España · Interior', `${e.crime.from} … ${e.crime.to}`], ['España · INE renta', e.income.from], ['INE · secciones', e.sections.from]);
  }
  const dl = $('ranges');
  dl.replaceChildren();
  for (const [k, v] of rows) dl.append(el('dt', { textContent: k }), el('dd', { textContent: v }));
  dl.append(el('dt', { textContent: t.built }), el('dd', { textContent: meta.generated_at.slice(0, 10) }));
}

// One entry per publisher, each dataset title once, linking to the files the
// pipeline downloaded; plus the geocoder, which is the only other source.
function renderSources(): void {
  const byPortal = new Map<string, Map<string, string>>();
  for (const s of [data.bcn, data.madrid, data.spain].flatMap((m) => m?.sources ?? [])) {
    const titles = byPortal.get(s.portal) ?? new Map<string, string>();
    if (!titles.has(s.title)) titles.set(s.title, s.url);
    byPortal.set(s.portal, titles);
  }
  const list = $('sources');
  list.replaceChildren();
  const portal = (name: string, home: string | undefined, note: string | undefined): HTMLLIElement => {
    const li = el('li');
    const head = el('div', { className: 'portal' });
    head.append(home ? el('a', { href: home, target: '_blank', rel: 'noopener', textContent: name }) : name);
    li.append(head);
    if (note) li.append(el('div', { className: 'note', textContent: note }));
    return li;
  };
  for (const [name, titles] of byPortal) {
    const li = portal(name.replace(' (Socrata)', ''), PORTALS[name]?.home, PORTALS[name]?.note);
    const ul = el('ul');
    for (const [title, url] of titles) {
      const item = el('li');
      item.append(el('a', { href: url, target: '_blank', rel: 'noopener', textContent: title }));
      ul.append(item);
    }
    li.append(ul);
    list.append(li);
  }
  list.append(portal(`OpenStreetMap Nominatim — ${t.addrLookup}`, 'https://nominatim.openstreetmap.org', '© OpenStreetMap contributors, ODbL'));
}

async function renderStatus(): Promise<void> {
  const s = await dataStatus();
  const day = (time: number): string => new Date(time).toISOString().slice(0, 10);
  let text = `${t.release} ${s.version?.slice(0, 10) ?? '?'} · ${s.downloaded ? t.downloaded : t.bundled}`;
  if (s.checked !== null) text += ` · ${t.checked} ${day(s.checked)}`;
  $('dataStatus').textContent = text;
}

function showView(settingsView: boolean): void {
  $('mainView').hidden = settingsView;
  $('settingsView').hidden = !settingsView;
  $('back').hidden = !settingsView;
  $('openSettings').hidden = settingsView;
  window.scrollTo(0, 0);
}

async function main(): Promise<void> {
  settings = await loadSettings();

  const lang = $<HTMLSelectElement>('lang');
  lang.append(el('option', { value: '', id: 'autoLang' }), ...LANGS.map((l) => el('option', { value: l, textContent: LANG_NAMES[l] })));
  lang.value = settings.lang ?? '';
  lang.addEventListener('change', () => {
    settings.lang = (lang.value || null) as Lang | null;
    save();
    applyLang();
  });
  applyLang();

  $<HTMLInputElement>('on').addEventListener('change', (e) => {
    settings.on = (e.target as HTMLInputElement).checked;
    save();
    renderPower();
  });
  $('openSettings').addEventListener('click', () => showView(true));
  $('back').addEventListener('click', () => showView(false));
  $('openSources').addEventListener('click', () => $<HTMLDialogElement>('sourcesDialog').showModal());
  $('closeSources').addEventListener('click', () => $<HTMLDialogElement>('sourcesDialog').close());

  $<HTMLFormElement>('homeForm').addEventListener('submit', (e) => void setHome(e));
  $('homeClear').addEventListener('click', () => {
    settings.home = null;
    save();
    renderHome();
  });
  for (const key of ['badges', 'overlay', 'hideRed', 'hideLowPop'] as const) {
    $<HTMLInputElement>(key).addEventListener('change', (e) => {
      settings[key] = (e.target as HTMLInputElement).checked;
      if (key === 'hideRed') $<HTMLInputElement>('hideBelow').disabled = !settings.hideRed;
      save();
    });
  }
  $<HTMLInputElement>('hideBelow').addEventListener('input', (e) => {
    const v = (e.target as HTMLInputElement).valueAsNumber;
    if (Number.isNaN(v)) return;
    settings.hideBelow = Math.max(0, Math.min(100, Math.round(v)));
    save();
  });
  $<HTMLInputElement>('opacity').addEventListener('input', (e) => {
    settings.opacity = Number((e.target as HTMLInputElement).value);
    $<HTMLOutputElement>('opacityOut').value = `${Math.round(settings.opacity * 100)}%`;
    save();
  });
  $('reset').addEventListener('click', () => {
    // Not display settings: keep the address, the language and the master switch.
    settings = { ...structuredClone(DEFAULTS), home: settings.home, lang: settings.lang, on: settings.on };
    renderControls();
    save();
  });
  $('dataCheck').addEventListener('click', async (e) => {
    const btn = e.currentTarget as HTMLButtonElement;
    btn.disabled = true;
    $('dataStatus').textContent = t.checking;
    const reply = (await chrome.runtime.sendMessage({ type: REFRESH_MSG }).catch(() => 'failed')) as string;
    btn.disabled = false;
    // A new release applies to pages loaded from now on, and to this popup when reopened.
    if (reply.startsWith('updated')) $('dataStatus').textContent = t.updated;
    else {
      await renderStatus();
      $('dataStatus').textContent += ` · ${reply}`;
    }
  });

  const [meta, madrid, spain] = await Promise.all([
    readData<Meta>('bcn/meta.json'),
    readData<Meta>('madrid/meta.json'),
    readData<{ meta: Meta }>('spain/index.json'),
  ]);
  data = { bcn: meta, madrid, spain: spain?.meta ?? null };
  renderMeta();
  renderSources();
}

void main();

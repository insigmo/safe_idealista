import { dataStatus, readData, REFRESH_MSG } from './data';
import type { Meta } from './grid';
import { geocode } from './home';
import { CATEGORIES, CATEGORY_LABELS, DEFAULTS, loadSettings, saveSettings, type Settings } from './settings';

const $ = <T extends HTMLElement>(id: string): T => document.getElementById(id) as T;

let settings: Settings;

function save(): void {
  void saveSettings(settings);
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
    const row = document.createElement('div');
    row.className = 'cat';
    const label = document.createElement('label');
    const box = Object.assign(document.createElement('input'), { type: 'checkbox', checked: settings.enabled[c] });
    label.append(box, ` ${CATEGORY_LABELS[c]}${c === 'crime' ? '*' : ''}`);
    const slider = Object.assign(document.createElement('input'), {
      type: 'range', min: '0', max: '100', step: '5', value: String(settings.weights[c]), disabled: !settings.enabled[c],
    });
    slider.setAttribute('aria-label', `${CATEGORY_LABELS[c]} weight`);
    const out = document.createElement('output');
    out.value = String(settings.weights[c]);
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
    row.append(label, slider, out);
    cats.append(row);
  }
  for (const text of [
    'Dirt, graffiti and incivism exist in Barcelona and Madrid, smell only in Barcelona; low income only outside the two cities.',
    '* Crime: in Barcelona, per district split over barris by estimate; in Madrid, city-police incidents per barrio; elsewhere, one figure per municipality, published only above 20,000 residents.',
  ]) {
    cats.append(Object.assign(document.createElement('p'), { className: 'small', textContent: text }));
  }
}

function renderHome(): void {
  const status = $('homeStatus');
  status.classList.remove('error');
  status.textContent = settings.home ? `Found: ${settings.home.label}` : '';
  $<HTMLInputElement>('homeInput').value = settings.home?.address ?? '';
  $('homeClear').hidden = !settings.home;
}

async function setHome(e: SubmitEvent): Promise<void> {
  e.preventDefault();
  const query = $<HTMLInputElement>('homeInput').value.trim();
  const status = $('homeStatus');
  if (!query) return;
  const button = $<HTMLButtonElement>('homeSet');
  button.disabled = true;
  status.classList.remove('error');
  status.textContent = 'Looking up…';
  try {
    const home = await geocode(query);
    if (!home) {
      status.classList.add('error');
      status.textContent = 'Address not found in Spain. Try adding the city, or type coordinates.';
      return;
    }
    settings.home = home;
    save();
    renderHome();
  } catch (err) {
    status.classList.add('error');
    status.textContent = `Lookup failed (${err instanceof Error ? err.message : String(err)}). Type coordinates instead.`;
  } finally {
    button.disabled = false;
  }
}

function renderMeta(meta: Meta, madrid: Meta | null, spain: Meta | null): void {
  const r = meta.date_ranges;
  const rows: [string, string][] = [
    ['BCN complaints (IRIS)', `${r.iris.from} … ${r.iris.to}`],
    ['BCN crime (Mossos)', `${r.crime.from} … ${r.crime.to}`],
    ['BCN crime split (GUB)', `${r.gub_incidents.from} … ${r.gub_incidents.to}`],
    ['BCN residents', r.population.from],
  ];
  if (madrid) {
    const m = madrid.date_ranges;
    rows.push(['Madrid reports (Avisos)', `${m.avisos.from} … ${m.avisos.to}`], ['Madrid police incidents', `${m.police_incidents.from} … ${m.police_incidents.to}`]);
  }
  if (spain) {
    const e = spain.date_ranges;
    rows.push(['Spain crime (Interior)', `${e.crime.from} … ${e.crime.to}`], ['Spain income (INE)', e.income.from], ['Census sections', e.sections.from]);
  }
  rows.push(['Generated', meta.generated_at.slice(0, 10)]);
  const dl = $('ranges');
  for (const [k, v] of rows) {
    dl.append(Object.assign(document.createElement('dt'), { textContent: k }), Object.assign(document.createElement('dd'), { textContent: v }));
  }
  for (const c of [...meta.caveats, ...(madrid?.caveats ?? []), ...(spain?.caveats ?? [])]) {
    $('caveats').append(Object.assign(document.createElement('li'), { textContent: c }));
  }
  // Both extra layers list the same INE files; show each source once.
  const seen = new Set<string>();
  for (const s of [...meta.sources, ...(madrid?.sources ?? []), ...(spain?.sources ?? [])]) {
    const text = `${s.dataset} — ${s.resource}`;
    if (seen.has(text)) continue;
    seen.add(text);
    $('sources').append(Object.assign(document.createElement('li'), { textContent: text }));
  }
}

async function main(): Promise<void> {
  settings = await loadSettings();
  renderControls();
  renderHome();
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
    // The address is not a display setting: keep it.
    settings = { ...structuredClone(DEFAULTS), home: settings.home };
    renderControls();
    save();
  });
  $('dataCheck').addEventListener('click', async (e) => {
    const btn = e.currentTarget as HTMLButtonElement;
    btn.disabled = true;
    $('dataStatus').textContent = 'Checking…';
    const reply = (await chrome.runtime.sendMessage({ type: REFRESH_MSG }).catch(() => 'failed')) as string;
    btn.disabled = false;
    // A new release applies to pages loaded from now on, and to this popup when reopened.
    $('dataStatus').textContent = reply.startsWith('updated') ? `${reply}. Reload Idealista pages to use it.` : `Release check: ${reply}.`;
  });
  void renderStatus();
  const [meta, madrid, spain] = await Promise.all([
    readData<Meta>('bcn/meta.json'),
    readData<Meta>('madrid/meta.json'),
    readData<{ meta: Meta }>('spain/index.json'),
  ]);
  if (meta) renderMeta(meta, madrid, spain?.meta ?? null);
}

async function renderStatus(): Promise<void> {
  const s = await dataStatus();
  const day = (t: number): string => new Date(t).toISOString().slice(0, 10);
  const parts = [`Data release ${s.version?.slice(0, 10) ?? '?'}, ${s.downloaded ? 'downloaded' : 'bundled'}`];
  if (s.checked !== null) parts.push(`last checked ${day(s.checked)}`);
  $('dataStatus').textContent = `${parts.join('; ')}.`;
}

void main();

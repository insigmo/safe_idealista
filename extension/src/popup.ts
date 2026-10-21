import type { Meta } from './grid';
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
    'Dirt, smell, graffiti and incivism exist only in Barcelona; low income only outside it.',
    '* Crime: in Barcelona, per district split over barris by estimate; elsewhere, one figure per municipality, published only above 20,000 residents.',
  ]) {
    cats.append(Object.assign(document.createElement('p'), { className: 'small', textContent: text }));
  }
}

function renderMeta(meta: Meta, spain: Meta | null): void {
  const r = meta.date_ranges;
  const rows: [string, string][] = [
    ['BCN complaints (IRIS)', `${r.iris.from} … ${r.iris.to}`],
    ['BCN crime (Mossos)', `${r.crime.from} … ${r.crime.to}`],
    ['BCN crime split (GUB)', `${r.gub_incidents.from} … ${r.gub_incidents.to}`],
    ['BCN residents', r.population.from],
  ];
  if (spain) {
    const e = spain.date_ranges;
    rows.push(['Spain crime (Interior)', `${e.crime.from} … ${e.crime.to}`], ['Spain income (INE)', e.income.from], ['Census sections', e.sections.from]);
  }
  rows.push(['Generated', meta.generated_at.slice(0, 10)]);
  const dl = $('ranges');
  for (const [k, v] of rows) {
    dl.append(Object.assign(document.createElement('dt'), { textContent: k }), Object.assign(document.createElement('dd'), { textContent: v }));
  }
  for (const c of [...meta.caveats, ...(spain?.caveats ?? [])]) $('caveats').append(Object.assign(document.createElement('li'), { textContent: c }));
  for (const s of [...meta.sources, ...(spain?.sources ?? [])]) {
    $('sources').append(Object.assign(document.createElement('li'), { textContent: `${s.dataset} — ${s.resource}` }));
  }
}

async function main(): Promise<void> {
  settings = await loadSettings();
  renderControls();
  for (const key of ['badges', 'overlay', 'hideRed', 'hideLowPop'] as const) {
    $<HTMLInputElement>(key).addEventListener('change', (e) => {
      settings[key] = (e.target as HTMLInputElement).checked;
      save();
    });
  }
  $<HTMLInputElement>('opacity').addEventListener('input', (e) => {
    settings.opacity = Number((e.target as HTMLInputElement).value);
    $<HTMLOutputElement>('opacityOut').value = `${Math.round(settings.opacity * 100)}%`;
    save();
  });
  $('reset').addEventListener('click', () => {
    settings = structuredClone(DEFAULTS);
    renderControls();
    save();
  });
  const meta = (await (await fetch('meta.json')).json()) as Meta;
  const spain = await fetch('spain/index.json')
    .then((r) => (r.ok ? (r.json() as Promise<{ meta: Meta }>) : null))
    .catch(() => null);
  renderMeta(meta, spain?.meta ?? null);
}

void main();

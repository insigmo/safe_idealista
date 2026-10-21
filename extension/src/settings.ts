// income exists only in the Spain-wide layer (outside Barcelona's grid); the
// Barcelona layers dirt…incivism exist only there. crime exists in both.
export const CATEGORIES = ['dirt', 'smell', 'graffiti', 'incivism', 'crime', 'income'] as const;
export type Category = (typeof CATEGORIES)[number];

export const CATEGORY_LABELS: Record<Category, string> = {
  dirt: 'Dirt',
  smell: 'Smell',
  graffiti: 'Graffiti',
  incivism: 'Incivism',
  crime: 'Crime',
  income: 'Low income',
};

export interface Settings {
  badges: boolean;
  overlay: boolean;
  opacity: number; // 0..1
  hideRed: boolean;
  hideLowPop: boolean; // overlay: skip cells with <100 residents
  enabled: Record<Category, boolean>;
  weights: Record<Category, number>; // 0..100, relative
}

export const DEFAULTS: Settings = {
  badges: true,
  overlay: true,
  opacity: 0.45,
  hideRed: false,
  hideLowPop: true,
  enabled: { dirt: true, smell: true, graffiti: true, incivism: true, crime: true, income: true },
  weights: { crime: 35, smell: 20, dirt: 20, graffiti: 15, incivism: 10, income: 15 },
};

const KEY = 'settings';

function merge(stored: Partial<Settings> | undefined): Settings {
  return {
    ...DEFAULTS,
    ...stored,
    enabled: { ...DEFAULTS.enabled, ...stored?.enabled },
    weights: { ...DEFAULTS.weights, ...stored?.weights },
  };
}

export async function loadSettings(): Promise<Settings> {
  const got = await chrome.storage.sync.get(KEY);
  return merge(got[KEY] as Partial<Settings> | undefined);
}

export async function saveSettings(s: Settings): Promise<void> {
  await chrome.storage.sync.set({ [KEY]: s });
}

export function onSettingsChanged(cb: (s: Settings) => void): void {
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === 'sync' && changes[KEY]) cb(merge(changes[KEY].newValue as Partial<Settings>));
  });
}

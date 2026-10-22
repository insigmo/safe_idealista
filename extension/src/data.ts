// Data files (bcn/, madrid/, spain/, listed in release.json; see pack.mjs)
// come from one of two places: the copy bundled with the extension, or a
// newer release the background worker downloaded into chrome.storage.local
// (background.ts). A download stores only the files whose contents differ
// from the bundled copy, so a missing key means "same as bundled".

// Must match SCHEMA in pack.mjs; releases with another schema are ignored.
export const SCHEMA = 1;

export const RELEASE_BASE = 'https://raw.githubusercontent.com/insigmo/safe_idealista/data-release/';

export interface Release {
  schema: number;
  version: string; // ISO timestamp, ordered as a string
  files: Record<string, { size: number; sha256: string }>;
}

// storage.local keys: the installed download's release.json, the time of the
// last check, and one key per downloaded file.
export const INSTALLED = 'data:release';
export const CHECKED = 'data:checked';
export const fileKey = (version: string, path: string): string => `data:${version}:${path}`;

let bundled: Promise<Release | null> | null = null;

export function bundledRelease(): Promise<Release | null> {
  bundled ??= fetch(chrome.runtime.getURL('release.json'))
    .then((r) => (r.ok ? (r.json() as Promise<Release>) : null))
    .catch(() => null);
  return bundled;
}

export async function installedRelease(): Promise<Release | null> {
  const got = await chrome.storage.local.get(INSTALLED);
  const r = got[INSTALLED] as Release | undefined;
  return r && r.schema === SCHEMA ? r : null;
}

let active: Promise<Release | null> | null = null;

// The download to read from, or null to read the bundled copy. Settled once
// per page, so a release installed meanwhile applies from the next load.
function activeDownload(): Promise<Release | null> {
  active ??= Promise.all([installedRelease(), bundledRelease()])
    .then(([got, own]) => (got && (!own || got.version > own.version) ? got : null))
    .catch(() => null);
  return active;
}

export async function readData<T>(path: string): Promise<T | null> {
  const dl = await activeDownload();
  if (dl) {
    const key = fileKey(dl.version, path);
    try {
      const got = await chrome.storage.local.get(key);
      if (got[key] !== undefined) return got[key] as T;
    } catch {
      // fall through to the bundled copy
    }
  }
  try {
    const r = await fetch(chrome.runtime.getURL(path));
    return r.ok ? ((await r.json()) as T) : null;
  } catch {
    return null;
  }
}

// What the popup shows: the release in use and whether it was downloaded.
export async function dataStatus(): Promise<{ version: string | null; downloaded: boolean; checked: number | null }> {
  const [dl, own, got] = await Promise.all([activeDownload(), bundledRelease(), chrome.storage.local.get(CHECKED)]);
  return {
    version: dl?.version ?? own?.version ?? null,
    downloaded: dl !== null,
    checked: typeof got[CHECKED] === 'number' ? (got[CHECKED] as number) : null,
  };
}

// Popup → background: check for a release now. The reply is a short status.
export const REFRESH_MSG = 'data-refresh';

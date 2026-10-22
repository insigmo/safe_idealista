// Keeps the data current: once a day, reads release.json from the
// data-release branch (published monthly by .github/workflows/data-release.yml)
// and, when it is newer than what is in use, downloads the files whose
// contents changed. Each file is checked against its SHA-256 before it is
// stored; the new release becomes active only once every file is in place,
// so pages never mix a half-downloaded release with the old one.
import {
  bundledRelease, CHECKED, fileKey, INSTALLED, installedRelease, REFRESH_MSG, RELEASE_BASE, SCHEMA, type Release,
} from './data';

const ALARM = 'data-refresh';
const DAY_MIN = 24 * 60;

async function schedule(): Promise<void> {
  if (!(await chrome.alarms.get(ALARM))) await chrome.alarms.create(ALARM, { delayInMinutes: 1, periodInMinutes: DAY_MIN });
}

chrome.runtime.onInstalled.addListener(() => void schedule());
chrome.runtime.onStartup.addListener(() => void schedule());
chrome.alarms.onAlarm.addListener((a) => {
  if (a.name === ALARM) refresh().catch((e) => console.warn('[BCN-SQ] data refresh failed', e));
});
chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  if (msg?.type !== REFRESH_MSG) return false;
  refresh().then(reply, (e: unknown) => reply(`failed: ${e instanceof Error ? e.message : String(e)}`));
  return true; // reply is async
});

let running: Promise<string> | null = null;

function refresh(): Promise<string> {
  running ??= update().finally(() => (running = null));
  return running;
}

async function fetchJson<T>(url: string): Promise<T> {
  const r = await fetch(url, { cache: 'no-cache' });
  if (!r.ok) throw new Error(`${url}: HTTP ${r.status}`);
  return (await r.json()) as T;
}

async function sha256(bytes: ArrayBuffer): Promise<string> {
  const d = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));
  return Array.from(d, (b) => b.toString(16).padStart(2, '0')).join('');
}

async function update(): Promise<string> {
  const remote = await fetchJson<Release>(`${RELEASE_BASE}release.json`);
  await chrome.storage.local.set({ [CHECKED]: Date.now() });
  if (remote.schema !== SCHEMA) return `release needs a newer extension (schema ${remote.schema})`;
  const [own, cur] = await Promise.all([bundledRelease(), installedRelease()]);
  const curInUse = cur && (!own || cur.version > own.version) ? cur : null;
  // A download older than the bundled copy (after an extension update) is dead weight.
  if (cur && !curInUse) await chrome.storage.local.remove(INSTALLED);
  await prune(curInUse?.version ?? null); // also leftovers of an interrupted download
  const inUse = curInUse ?? own;
  if (inUse && remote.version <= inUse.version) return 'up to date';

  const paths = Object.keys(remote.files);
  let fetched = 0;
  const work = paths.map((path) => async () => {
    const f = remote.files[path];
    if (own?.files[path]?.sha256 === f.sha256) return; // the bundled copy serves it
    const key = fileKey(remote.version, path);
    if (curInUse && curInUse.files[path]?.sha256 === f.sha256) {
      const old = fileKey(curInUse.version, path);
      const got = await chrome.storage.local.get(old);
      if (got[old] !== undefined) {
        await chrome.storage.local.set({ [key]: got[old] });
        return;
      }
    }
    const r = await fetch(RELEASE_BASE + path, { cache: 'no-cache' });
    if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`);
    const bytes = await r.arrayBuffer();
    // Right after a publish the CDN can still serve an old file: refuse it
    // and try again on the next check.
    if (bytes.byteLength !== f.size || (await sha256(bytes)) !== f.sha256) throw new Error(`${path}: checksum mismatch`);
    await chrome.storage.local.set({ [key]: JSON.parse(new TextDecoder().decode(bytes)) });
    fetched++;
  });
  // A few at a time: the files are small but there are ~80 of them.
  await Promise.all(Array.from({ length: 4 }, async () => {
    for (let job = work.shift(); job; job = work.shift()) await job();
  }));

  await chrome.storage.local.set({ [INSTALLED]: remote });
  await prune(remote.version);
  return `updated to ${remote.version.slice(0, 10)} (${fetched} files downloaded)`;
}

// Drops stored files of every release but `keep`.
async function prune(keep: string | null): Promise<void> {
  const keys = chrome.storage.local.getKeys
    ? await chrome.storage.local.getKeys()
    : Object.keys(await chrome.storage.local.get(null));
  const own = keep ? `data:${keep}:` : null;
  const stale = keys.filter((k) => k.startsWith('data:') && k !== INSTALLED && k !== CHECKED && !(own && k.startsWith(own)));
  if (stale.length) await chrome.storage.local.remove(stale);
}

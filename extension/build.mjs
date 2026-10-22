// Builds the unpacked extension into dist/. `node build.mjs --harness` also
// builds dist-test/harness.js, a single script that runs the content and map
// scripts on a live page with a fake chrome.storage (see test/harness.ts).
import { build } from 'esbuild';
import { copyFileSync, cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { packData } from './pack.mjs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(fileURLToPath(import.meta.url));
const dist = join(root, 'dist');
// The pipeline's output, packed as the bundled data release (pack.mjs): read
// on demand as web-accessible resources unless a newer download exists.
const dataDir = join(root, '..', 'data');

const common = {
  bundle: true,
  format: 'iife',
  target: ['chrome120', 'firefox140'],
  minify: true,
  legalComments: 'none',
  logLevel: 'warning',
};

rmSync(dist, { recursive: true, force: true });
mkdirSync(dist, { recursive: true });

await build({
  ...common,
  entryPoints: { background: 'src/background.ts', content: 'src/content.ts', page: 'src/page.ts', popup: 'src/popup.ts' },
  outdir: dist,
  absWorkingDir: root,
});

for (const f of ['manifest.json', 'popup.html', 'popup.css', 'content.css']) {
  copyFileSync(join(root, 'src', f), join(dist, f));
}
packData(dataDir, dist);

// Firefox: the same files with its own manifest. Firefox runs MV3 background
// code as an event page (background.scripts, no service_worker), needs a
// fixed add-on id for storage.sync and AMO, and wants the data that leaves
// the browser declared: the home address goes to Nominatim (src/home.ts).
// 140 is the first version that reads data_collection_permissions.
const distFirefox = join(root, 'dist-firefox');
rmSync(distFirefox, { recursive: true, force: true });
cpSync(dist, distFirefox, { recursive: true });
const manifest = JSON.parse(readFileSync(join(dist, 'manifest.json'), 'utf8'));
manifest.background = { scripts: ['background.js'] };
manifest.browser_specific_settings = {
  gecko: {
    id: 'street-quality-spain@insigmo.github.io',
    strict_min_version: '140.0',
    data_collection_permissions: { required: ['locationInfo'] },
  },
  gecko_android: { strict_min_version: '142.0' },
};
writeFileSync(join(distFirefox, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');

if (process.argv.includes('--harness')) {
  await build({
    ...common,
    minify: false,
    entryPoints: { harness: 'test/harness.ts' },
    outdir: join(root, 'dist-test'),
    absWorkingDir: root,
    loader: { '.css': 'text' },
  });
  for (const f of ['fixture.html', 'detail.html']) copyFileSync(join(root, 'test', f), join(root, 'dist-test', f));
  // Popup preview: the real popup files plus the fake chrome.storage.
  await build({ ...common, minify: false, entryPoints: { shim: 'test/shim.ts' }, outdir: join(root, 'dist-test'), absWorkingDir: root });
  for (const f of ['popup.js', 'popup.css', 'release.json']) copyFileSync(join(dist, f), join(root, 'dist-test', f));
  for (const d of ['bcn', 'spain', 'madrid']) cpSync(join(dist, d), join(root, 'dist-test', d), { recursive: true });
  const popupHtml = readFileSync(join(dist, 'popup.html'), 'utf8').replace('<script src="popup.js">', '<script src="shim.js"></script><script src="popup.js">');
  writeFileSync(join(root, 'dist-test', 'popup.html'), popupHtml);
}
console.log('built', dist, 'and', distFirefox);

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
  target: 'chrome120',
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
console.log('built', dist);

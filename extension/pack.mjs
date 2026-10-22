// Lays out the pipeline's output (../data) as the data files the extension
// reads: bcn/, madrid/ and spain/, plus release.json listing each file with
// its size and SHA-256. build.mjs packs them into dist/ (the bundled copy);
// the monthly workflow packs them into the data-release branch, from which
// the background worker downloads newer releases (src/background.ts).
//
//   node pack.mjs <data dir> <out dir>
import { createHash } from 'node:crypto';
import { copyFileSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Bump together with SCHEMA in src/data.ts when the file layout or the JSON
// shape changes in a way older extensions cannot read.
export const SCHEMA = 1;

export function packData(dataDir, outDir) {
  const files = {};
  const put = (path, bytes) => {
    mkdirSync(dirname(join(outDir, path)), { recursive: true });
    writeFileSync(join(outDir, path), bytes);
    files[path] = { size: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') };
  };
  const metas = [];
  // The grids go with their meta split out, so the popup need not read them.
  for (const [layer, src] of [['bcn', join(dataDir, 'grid.json')], ['madrid', join(dataDir, 'madrid', 'grid.json')]]) {
    const bytes = readFileSync(src);
    const { meta } = JSON.parse(bytes.toString('utf8'));
    metas.push(meta);
    put(`${layer}/grid.json`, bytes);
    put(`${layer}/meta.json`, Buffer.from(JSON.stringify(meta)));
  }
  const spainDir = join(dataDir, 'spain');
  for (const f of readdirSync(spainDir).filter((f) => f.endsWith('.json'))) {
    const bytes = readFileSync(join(spainDir, f));
    if (f === 'index.json') metas.push(JSON.parse(bytes.toString('utf8')).meta);
    put(`spain/${f}`, bytes);
  }
  // ISO timestamps, so the newest layer orders releases as plain strings.
  const version = metas.map((m) => m.generated_at).sort().at(-1);
  const release = { schema: SCHEMA, version, files };
  writeFileSync(join(outDir, 'release.json'), JSON.stringify(release));
  return release;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [dataDir, outDir] = process.argv.slice(2);
  if (!dataDir || !outDir) throw new Error('usage: node pack.mjs <data dir> <out dir>');
  const r = packData(dataDir, outDir);
  console.log(`packed ${Object.keys(r.files).length} files, version ${r.version}`);
}

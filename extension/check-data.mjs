// Refuses pipeline output well below the current coverage: a source that
// silently shrank (a renamed column, an empty year) must not reach users. Run
// before publishing by `task data` and .github/workflows/data-release.yml.
//
//   node check-data.mjs <data dir>
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const dataDir = process.argv[2];
if (!dataDir) throw new Error('usage: node check-data.mjs <data dir>');
const read = (f) => JSON.parse(readFileSync(join(dataDir, f), 'utf8'));

for (const [f, min] of Object.entries({ 'grid.json': 1000, 'madrid/grid.json': 5000 })) {
  const got = Object.keys(read(f).cells).length;
  if (got < min) throw new Error(`${f}: ${got} cells, expected at least ${min}`);
}
const shards = read('spain/index.json').shards.length;
if (shards < 70) throw new Error(`spain: ${shards} shards, expected at least 70`);
console.log('data coverage ok');

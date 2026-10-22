// Cuts a release from the local checkout: bumps "version" in
// src/manifest.json, rebuilds dist/ and dist-firefox/ (both committed),
// commits, tags v<version> and pushes master with the tag. The tag push is
// what makes .github/workflows/extension-package.yml attach the store zips
// to a GitHub release. Commit and tag go out under the local git identity;
// CI never writes to the repository.
//
//   npm run release [-- patch|minor|major|<x.y.z>] [-- --no-push]
import { execSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const manifestPath = join(here, 'src', 'manifest.json');
const args = process.argv.slice(2);
const push = !args.includes('--no-push');
const bump = args.find((a) => !a.startsWith('--')) ?? 'patch';

const sh = (cmd) => execSync(cmd, { cwd: here, stdio: ['ignore', 'pipe', 'inherit'] }).toString().trim();
const run = (cmd) => execSync(cmd, { cwd: here, stdio: 'inherit' });
const fail = (msg) => { console.error(msg); process.exit(1); };

if (sh('git rev-parse --abbrev-ref HEAD') !== 'master') fail('release from master only');
if (sh('git status --porcelain')) fail('working tree is not clean');

const text = readFileSync(manifestPath, 'utf8');
const current = JSON.parse(text).version;
const [maj, min, pat] = current.split('.').map(Number);
const next = { patch: `${maj}.${min}.${pat + 1}`, minor: `${maj}.${min + 1}.0`, major: `${maj + 1}.0.0` }[bump] ?? bump;
if (!/^\d+\.\d+\.\d+$/.test(next)) fail(`not a version or bump kind: ${bump}`);
const tag = `v${next}`;
if (sh(`git tag -l ${tag}`)) fail(`tag ${tag} already exists`);

// Replace in place, so the manifest keeps its formatting.
writeFileSync(manifestPath, text.replace(`"version": "${current}"`, `"version": "${next}"`));
run('npm run build');
run('git add src/manifest.json dist dist-firefox');
run(`git commit -m "release ${tag}"`);
run(`git tag -a ${tag} -m "${tag}"`);
if (push) run(`git push --atomic origin master ${tag}`);
console.log(`${current} -> ${next}${push ? ', pushed' : ', not pushed (git push --atomic origin master ' + tag + ')'}`);

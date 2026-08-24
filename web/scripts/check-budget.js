#!/usr/bin/env node
/**
 * Enforce the frontend performance budget.
 *
 * A budget that only warns is a budget that gets exceeded. This exits non-zero,
 * so a change that regresses the bundle fails CI rather than being noticed six
 * releases later when someone wonders why the app got slow.
 *
 * The 100 KB figure is not arbitrary: it is roughly one second of parse and
 * execute on a low-end CPU, which is the difference between "instant" and
 * "sluggish" on the machines this product is meant to run well on.
 */

import { gzipSync } from 'node:zlib';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const BUDGETS = {
  // Total client JavaScript, gzipped, for a first load.
  clientJs: 100 * 1024,
  // Total CSS, gzipped.
  css: 20 * 1024
};

const CLIENT_DIR = '.svelte-kit/output/client/_app';

function walk(dir) {
  const out = [];
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...walk(full));
    else out.push(full);
  }
  return out;
}

function gzippedSize(paths) {
  if (paths.length === 0) return 0;
  // Concatenate before compressing: separate files each carry their own gzip
  // header, and the browser fetches them over one connection anyway. Measuring
  // them individually would overstate the total.
  const combined = Buffer.concat(paths.map((p) => readFileSync(p)));
  return gzipSync(combined).length;
}

const files = walk(CLIENT_DIR);
if (files.length === 0) {
  console.error(`No build output in ${CLIENT_DIR}. Run "npm run build" first.`);
  process.exit(1);
}

const js = files.filter((f) => f.endsWith('.js'));
const css = files.filter((f) => f.endsWith('.css'));

const results = [
  { name: 'client JS', actual: gzippedSize(js), budget: BUDGETS.clientJs, count: js.length },
  { name: 'CSS', actual: gzippedSize(css), budget: BUDGETS.css, count: css.length }
];

const kb = (n) => `${(n / 1024).toFixed(1)} KB`;
let failed = false;

console.log('\nPerformance budget (gzipped)\n');
for (const r of results) {
  const pct = Math.round((r.actual / r.budget) * 100);
  const ok = r.actual <= r.budget;
  if (!ok) failed = true;
  console.log(
    `  ${ok ? '✓' : '✗'} ${r.name.padEnd(10)} ${kb(r.actual).padStart(9)} / ${kb(r.budget).padStart(9)}` +
      `  (${pct}% of budget, ${r.count} files)`
  );
}
console.log('');

if (failed) {
  console.error(
    'Budget exceeded.\n\n' +
      'Raising a budget is a legitimate decision, but it must be a deliberate one:\n' +
      'edit web/scripts/check-budget.js and say in the PR what the extra bytes buy.\n' +
      'See docs/architecture/frontend-architecture.md §2.\n'
  );
  process.exit(1);
}

console.log('Within budget.\n');

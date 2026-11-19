// @ts-check
/* global process, console */
// The web build's native packages (PLAN task 22.14). Vite builds with Rolldown and Lightning CSS,
// which load a platform package that npm installs as an optional dependency. npm has been seen to
// skip one without an error (CI, 2026-11-17), and the build then fails far from the cause. Run
// after `npm ci`: it names every package that does not load and exits 1.
//   node scripts/check-native.mjs
//   node scripts/check-native.mjs --self-test

/** Packages whose import loads their native binding. */
export const NATIVE = ['rolldown', 'lightningcss'];

/**
 * The packages that fail to load, each with the first line of its error.
 * @param {(name: string) => Promise<unknown>} load
 * @returns {Promise<string[]>}
 */
export async function missing(load) {
  const out = [];
  for (const name of NATIVE) {
    try {
      await load(name);
    } catch (e) {
      out.push(`${name}: ${(e instanceof Error ? e.message : String(e)).split('\n')[0]}`);
    }
  }
  return out;
}

async function selfTest() {
  const noBinding = new Error("Cannot find module '../lightningcss.linux-x64-gnu.node'\nRequire stack: …");
  const cases = [
    ['everything loads', await missing(() => Promise.resolve()), []],
    [
      'a missing binding is named with its error',
      await missing((name) => (name === 'lightningcss' ? Promise.reject(noBinding) : Promise.resolve())),
      ["lightningcss: Cannot find module '../lightningcss.linux-x64-gnu.node'"],
    ],
    [
      'every missing one is named',
      await missing(() => Promise.reject(new Error('Cannot find native binding.'))),
      ['rolldown: Cannot find native binding.', 'lightningcss: Cannot find native binding.'],
    ],
  ];
  for (const [name, got, want] of cases) {
    if (JSON.stringify(got) !== JSON.stringify(want)) {
      console.error(`check-native self-test: ${String(name)}: got ${JSON.stringify(got)}`);
      process.exit(1);
    }
  }
  console.log(`check-native self-test: ok (${cases.length} cases)`);
}

if (process.argv[2] === '--self-test') {
  await selfTest();
} else {
  const bad = await missing((name) => import(name));
  if (bad.length) {
    console.error('check-native: npm ci left out a native package the build needs:');
    for (const line of bad) console.error(`  ${line}`);
    process.exit(1);
  }
  console.log(`check-native: ok (${NATIVE.join(', ')})`);
}

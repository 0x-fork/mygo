// Publishes the npm packages of a release: mygo-runtime, the platform
// packages of mygo-cli with freshly built binaries, then mygo-cli, which
// depends on them, once npm serves them. Versions already on npm are
// skipped, so a release that failed halfway can run again, and prereleases
// get the next dist-tag. Arguments go to npm publish, e.g. --dry-run or
// --provenance.
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { build, dir as cliDir, platformDir } from "../packages/cli/build.ts";
import { platforms } from "../packages/cli/index.js";

const root = join(import.meta.dir, "..");
const flags = process.argv.slice(2);

await build();
const first = [join(root, "packages", "runtime"), ...platforms.map(platformDir)];
for (const pkg of first) await publish(pkg);
// npm can take minutes to serve new packages, and a package manager
// installs mygo-cli without the platform packages it cannot fetch: bun then
// keeps them out of later installs while its lockfile lacks them.
if (!flags.includes("--dry-run")) {
  for (const pkg of first) await waitUntilServed(await manifest(pkg));
}
await publish(cliDir);

async function manifest(pkg: string): Promise<{ name: string; version: string }> {
  return JSON.parse(await readFile(join(pkg, "package.json"), "utf8"));
}

async function publish(pkg: string): Promise<void> {
  const { name, version } = await manifest(pkg);
  const view = Bun.spawnSync(["npm", "view", `${name}@${version}`, "version"], { stderr: "ignore" });
  if (view.stdout.toString().trim() === version) {
    console.log(`${name}@${version} is already published`);
    return;
  }
  const tag = version.includes("-") ? ["--tag", "next"] : [];
  const publish = Bun.spawnSync(["npm", "publish", "--access", "public", ...tag, ...flags], {
    cwd: pkg,
    stdout: "inherit",
    stderr: "inherit",
  });
  if (publish.exitCode !== 0) process.exit(publish.exitCode ?? 1);
}

/** Waits until npm serves name@version to package managers. */
async function waitUntilServed({ name, version }: { name: string; version: string }): Promise<void> {
  const deadline = Date.now() + 10 * 60_000;
  for (;;) {
    try {
      const res = await fetch(`https://registry.npmjs.org/${name.replace("/", "%2f")}`, {
        // The abbreviated metadata package managers install from.
        headers: { accept: "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*" },
      });
      const doc = res.ok ? ((await res.json()) as { versions?: Record<string, unknown> }) : {};
      if (doc.versions?.[version]) return;
    } catch {}
    if (Date.now() > deadline) {
      console.error(`npm does not serve ${name}@${version}: run the release again later`);
      process.exit(1);
    }
    console.log(`waiting for npm to serve ${name}@${version}`);
    await Bun.sleep(10_000);
  }
}

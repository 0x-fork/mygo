// Publishes the npm packages of a release: mygo-runtime, the platform
// packages of mygo-cli with freshly built binaries, then mygo-cli, which
// depends on them. Versions already on npm are skipped, so a release that
// failed halfway can run again, and prereleases get the next dist-tag.
// Arguments go to npm publish, e.g. --dry-run or --provenance.
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { build, dir as cliDir, platformDir } from "../packages/cli/build.ts";
import { platforms } from "../packages/cli/index.js";

const root = join(import.meta.dir, "..");
const flags = process.argv.slice(2);

await build();
for (const pkg of [join(root, "packages", "runtime"), ...platforms.map(platformDir), cliDir]) {
  const { name, version } = JSON.parse(await readFile(join(pkg, "package.json"), "utf8"));
  const view = Bun.spawnSync(["npm", "view", `${name}@${version}`, "version"], { stderr: "ignore" });
  if (view.stdout.toString().trim() === version) {
    console.log(`${name}@${version} is already published`);
    continue;
  }
  const tag = version.includes("-") ? ["--tag", "next"] : [];
  const publish = Bun.spawnSync(["npm", "publish", "--access", "public", ...tag, ...flags], {
    cwd: pkg,
    stdout: "inherit",
    stderr: "inherit",
  });
  if (publish.exitCode !== 0) process.exit(publish.exitCode ?? 1);
}

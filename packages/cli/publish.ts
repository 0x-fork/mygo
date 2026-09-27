// Publishes mygo-cli to npm: builds the binary of every platform, publishes
// the platform packages, then mygo-cli, which depends on them. Versions
// already on npm are skipped, so a release that failed halfway can be run
// again. Arguments are passed to npm publish, e.g. --dry-run or --tag next.
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { build, dir, platformDir } from "./build.ts";
import { platforms } from "./index.js";

const flags = process.argv.slice(2);
await build();
for (const pkg of [...platforms.map(platformDir), dir]) {
  const { name, version } = JSON.parse(await readFile(join(pkg, "package.json"), "utf8"));
  const view = Bun.spawnSync(["npm", "view", `${name}@${version}`, "version"], { stderr: "ignore" });
  if (view.stdout.toString().trim() === version) {
    console.log(`${name}@${version} is already published`);
    continue;
  }
  const publish = Bun.spawnSync(["npm", "publish", "--access", "public", ...flags], {
    cwd: pkg,
    stdout: "inherit",
    stderr: "inherit",
  });
  if (publish.exitCode !== 0) process.exit(publish.exitCode ?? 1);
}

import { expect, test } from "bun:test";
import { chmodSync, cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { build, dir, goTargets, platformDir, version } from "./build.ts";
import { platformPackage, platforms } from "./index.js";

const host = `${process.platform}-${process.arch}`;
const exe = process.platform === "win32" ? "mygo.exe" : "mygo";
const readJSON = (path: string) => JSON.parse(readFileSync(path, "utf8"));

test("platformPackage names the package of each platform", () => {
  expect(platformPackage("darwin", "arm64")).toBe("mygo-cli-darwin-arm64");
  expect(platformPackage("win32", "x64")).toBe("mygo-cli-win32-x64");
  expect(() => platformPackage("freebsd", "x64")).toThrow(/go install github.com\/egoist\/mygo\/cmd\/mygo@v/);
  expect(Object.keys(goTargets).sort()).toEqual([...platforms].sort());
});

test("the packages have the version of the Go module", async () => {
  const v = await version();
  const main = readJSON(join(dir, "package.json"));
  expect(main.version).toBe(v);
  expect(main.optionalDependencies).toEqual(Object.fromEntries(platforms.map((p) => [`mygo-cli-${p}`, v])));
  for (const platform of platforms) {
    const pkg = readJSON(join(platformDir(platform), "package.json"));
    const [os, cpu] = platform.split("-");
    expect(pkg).toMatchObject({ name: `mygo-cli-${platform}`, version: v, os: [os], cpu: [cpu] });
  }
});

// install lays out mygo-cli in node_modules as a package manager would, with
// or without the package of this platform, and returns the binary's path.
function install(root: string, withPlatform: boolean) {
  const cli = join(root, "node_modules", "mygo-cli");
  mkdirSync(join(cli, "bin"), { recursive: true });
  for (const file of ["package.json", "index.js", "bin/mygo.js"]) cpSync(join(dir, file), join(cli, file));
  if (withPlatform) {
    const pkg = join(root, "node_modules", `mygo-cli-${host}`);
    cpSync(platformDir(host), pkg, { recursive: true });
    return join(pkg, "bin", exe);
  }
}

function run(runtime: string, root: string, args: string[], env: Record<string, string> = {}) {
  const r = Bun.spawnSync([runtime, join(root, "node_modules", "mygo-cli", "bin", "mygo.js"), ...args], {
    env: { ...process.env, MYGO_CLI_BINARY: "", ...env },
  });
  return { code: r.exitCode, out: r.stdout.toString(), err: r.stderr.toString() };
}

const runtimes = ["node", "bun"].filter((r) => Bun.which(r));

test.skipIf(!platforms.includes(host))(
  "mygo runs the binary of the platform's package",
  async () => {
    await build([host]);
    const v = await version();
    const root = mkdtempSync(join(tmpdir(), "mygo-cli-"));
    try {
      const bin = install(root, true)!;
      if (process.platform !== "win32") chmodSync(bin, 0o644); // a package manager dropped the executable bit
      for (const runtime of runtimes) {
        expect(run(runtime, root, ["version"])).toMatchObject({ code: 0, out: `mygo ${v}\n` });
        expect(run(runtime, root, ["no-such-command"]).code).toBe(2);
      }
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  },
  120_000,
);

test("mygo explains a missing platform package", () => {
  const root = mkdtempSync(join(tmpdir(), "mygo-cli-"));
  try {
    install(root, false);
    for (const runtime of runtimes) {
      const r = run(runtime, root, ["version"]);
      expect(r.code).toBe(1);
      expect(r.err).toContain(`mygo-cli-${host}, the package with the mygo binary of this platform, is not installed`);
      // MYGO_CLI_BINARY points at a binary of one's own instead.
      const own = run(runtime, root, ["--version"], { MYGO_CLI_BINARY: process.execPath });
      expect(own.code).toBe(0);
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// mygo-cli ships the mygo command line tool of MyGo as a prebuilt binary
// per platform. Each binary is in a package of its own,
// @egoist/mygo-cli-<os>-<cpu>,
// which mygo-cli lists as optional dependencies: package managers install
// the one matching the machine's os and cpu fields.
import { execFileSync } from "node:child_process";
import { accessSync, chmodSync, constants, existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const here = dirname(fileURLToPath(import.meta.url));
const { version } = require("./package.json");

/** The platforms with a prebuilt binary, as `${process.platform}-${process.arch}`. */
export const platforms = ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64"];

const goInstall = `go install github.com/egoist/mygo/cmd/mygo@v${version}`;

/**
 * Returns the name of the package holding the mygo binary of a platform,
 * by default this one.
 */
export function platformPackage(platform = process.platform, arch = process.arch) {
  const target = `${platform}-${arch}`;
  if (!platforms.includes(target)) {
    throw new Error(`mygo-cli: there is no prebuilt mygo binary for ${target}; install the CLI with Go instead: ${goInstall}`);
  }
  return `@egoist/mygo-cli-${target}`;
}

/**
 * Returns the path of the mygo binary to run: $MYGO_CLI_BINARY when set,
 * else the binary of this platform's package. In a checkout of the MyGo
 * repository it builds the CLI from its source first.
 */
export function binaryPath() {
  if (process.env.MYGO_CLI_BINARY) return process.env.MYGO_CLI_BINARY;
  const exe = process.platform === "win32" ? "mygo.exe" : "mygo";
  const pkg = platformPackage();
  const checkout = join(here, "..", "..");
  if (isCheckout(checkout)) {
    // The platform packages hold no binary until they are released: build
    // the CLI from source, which Go's build cache makes quick.
    const bin = join(here, "npm", `${process.platform}-${process.arch}`, "bin", exe);
    execFileSync("go", ["build", "-o", bin, "./cmd/mygo"], { cwd: checkout, stdio: "inherit" });
    return bin;
  }
  let dir;
  try {
    dir = dirname(require.resolve(`${pkg}/package.json`));
  } catch {
    throw new Error(
      `mygo-cli: ${pkg}, the package with the mygo binary of this platform, is not installed. ` +
        `Reinstall mygo-cli without omitting optional dependencies, or install the CLI with Go: ${goInstall}`,
    );
  }
  const bin = join(dir, "bin", exe);
  if (process.platform !== "win32") {
    try {
      accessSync(bin, constants.X_OK);
    } catch {
      // Some package managers drop the executable bit.
      chmodSync(bin, 0o755);
    }
  }
  return bin;
}

/**
 * Types the configuration of mygo.config.ts: an object, or a function of the
 * command running that returns one.
 */
export function defineConfig(config) {
  return config;
}

/** Reports whether dir is a checkout of the MyGo repository. */
function isCheckout(dir) {
  try {
    return (
      readFileSync(join(dir, "go.mod"), "utf8").startsWith("module github.com/egoist/mygo\n") &&
      existsSync(join(dir, "cmd", "mygo", "main.go"))
    );
  } catch {
    return false;
  }
}

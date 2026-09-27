/** The platforms with a prebuilt binary, as `${process.platform}-${process.arch}`. */
export declare const platforms: readonly string[];

/**
 * Returns the name of the package holding the mygo binary of a platform,
 * by default this one. Throws for platforms without a prebuilt binary.
 */
export declare function platformPackage(platform?: string, arch?: string): string;

/**
 * Returns the path of the mygo binary to run: $MYGO_CLI_BINARY when set,
 * else the binary of this platform's package. Throws when that package is
 * not installed.
 */
export declare function binaryPath(): string;

/**
 * Types the configuration of mygo.config.ts, which takes the place of
 * mygo.json:
 *
 * ```ts
 * import { defineConfig } from "mygo-cli";
 *
 * export default defineConfig({
 *   name: "My App",
 *   identifier: "com.example.myapp",
 *   devUrl: "http://localhost:5173",
 * });
 * ```
 *
 * A function gets the command running and returns the configuration, or a
 * promise of it.
 */
export declare function defineConfig(config: Config): Config;
export declare function defineConfig(config: Promise<Config>): Promise<Config>;
export declare function defineConfig(config: ConfigFn): ConfigFn;

/** A configuration computed for the command running. */
export type ConfigFn = (env: ConfigEnv) => Config | Promise<Config>;

/** What a configuration function gets. */
export interface ConfigEnv {
  /** The mygo command running. */
  command: "dev" | "build" | "generate" | "init";
}

/** The configuration of a MyGo project. Every field is optional. */
export interface Config {
  /** The name users see: the app, its executable, menus and installers (default: the directory's name). */
  name?: string;
  /**
   * A reverse DNS name unique to the app, such as `com.example.myapp`, on
   * which systems key preferences, permissions and registrations (default:
   * `com.mygo.` and the name's letters and digits).
   */
  identifier?: string;
  /** The version of the app, which updates compare (default: `0.1.0`). */
  version?: string;
  /** The copyright notice of the app. */
  copyright?: string;
  /** A square PNG, ideally 1024×1024 (default: `resources/icon.png` when it exists). */
  icon?: string;
  /** The Go package of the app, relative to the project (default: `.`). */
  main?: string;
  /** Where `mygo build` writes the builds, one directory per platform (default: `dist`). */
  out?: string;
  /**
   * Where `mygo generate` writes the TypeScript client (default:
   * `src/mygo.ts` when package.json is at the project root).
   */
  bindings?: string;

  /**
   * The dev server `mygo dev` points the app at, such as
   * `http://localhost:5173`: windows load it for URLs without a scheme.
   */
  devUrl?: string;
  /** Runs in the project while `mygo dev` runs, e.g. the dev server; the app starts once `devUrl` answers. */
  devCommand?: string;
  /** Builds the frontend before `mygo build` compiles the app. */
  buildCommand?: string;
  /**
   * The directory of the built frontend, which `mygo build` embeds into the
   * app; `mygo dev` serves it from disk without `devUrl`.
   */
  frontendDist?: string;

  /**
   * Files and directories copied into the app under their base names, next
   * to the contents of the project's resources directory.
   */
  resources?: string[];
  /** The URL schemes the app opens, such as `myapp` for `myapp://…`, which reach `App.OnOpenURL`. */
  urlSchemes?: string[];
  /** The types of files the app opens, which reach `App.OnOpenFile`. */
  fileAssociations?: FileAssociation[];
  /** Signed updates, which `mygo.Updater` installs. */
  updates?: UpdatesConfig;
  /** macOS packaging. */
  macos?: MacOSConfig;
  /** Windows packaging. */
  windows?: WindowsConfig;
  /** Linux packaging. */
  linux?: LinuxConfig;
}

/** A type of file the app opens. */
export interface FileAssociation {
  /** The file name extensions, without dots, e.g. `["md"]`. */
  ext: string[];
  /** What the files are, e.g. `"Markdown Document"`. */
  name?: string;
  /** The MIME type of the files, by which Linux identifies them (default: `application/x-<app>-<ext>`). */
  mimeType?: string;
  /** The app's role for the files on macOS (default: `"Editor"`). */
  role?: "Editor" | "Viewer";
}

/** Signed updates: `publicKey` and one of `github` and `url` are required. */
export interface UpdatesConfig {
  /** The contents of mygo-update.pub, from `mygo keygen`. */
  publicKey: string;
  /** A public GitHub repository, `owner/name`, whose releases hold the updates. */
  github?: string;
  /** What precedes the version in release tags (default: `v`). */
  tagPrefix?: string;
  /** Instead of `github`, the HTTPS URL of a directory holding the updates. */
  url?: string;
  /** The path of mygo-update.key; `MYGO_UPDATER_PRIVATE_KEY`, holding the key, takes precedence. */
  privateKey?: string;
  /** A Markdown file whose `## <version>` section becomes the release notes (default: `CHANGELOG.md` when it exists). */
  changelog?: string;
}

/** macOS packaging. */
export interface MacOSConfig {
  /** The oldest macOS the app runs on (default: `12.0`). */
  minimumSystemVersion?: string;
  /** The identity that signs the app and its disk image, e.g. `Developer ID Application: Jane Doe (TEAMID)` (default: `-`, ad hoc). */
  signingIdentity?: string;
  /** A property list of entitlements to sign the app with. */
  entitlements?: string;
  /** Keys added to the app's Info.plist, replacing MyGo's, e.g. `NSCameraUsageDescription`. */
  infoPlist?: Record<string, unknown>;
  /** The volume name of the disk image (default: the name). */
  dmgTitle?: string;
  /** Notarizes production disk images. */
  notarize?: NotarizeConfig;
}

/** Credentials of `xcrun notarytool store-credentials <profile>`. */
export interface NotarizeConfig {
  /** The profile holding the credentials. */
  keychainProfile: string;
  /** The keychain holding the profile (default: the login keychain). */
  keychain?: string;
}

/** Windows packaging. */
export interface WindowsConfig {
  /** A code signing certificate (.pfx); its password comes from `MYGO_WINDOWS_CERTIFICATE_PASSWORD`. */
  certificate?: string;
  /** Instead of `certificate`, a command that signs a file, with `%1` for its path. */
  signCommand?: string;
  /** The time stamping server of `certificate` (default: `http://timestamp.digicert.com`). */
  timestampUrl?: string;
}

/** Linux packaging. */
export interface LinuxConfig {
  /** `Name <email>`: makes `mygo build` create a Debian package. */
  maintainer?: string;
  /** A short description, for the desktop entry and the package. */
  comment?: string;
  /** The categories of the desktop entry (default: `["Utility"]`). */
  categories?: string[];
  /** Debian packages the app needs besides GTK and WebKitGTK. */
  depends?: string[];
}

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

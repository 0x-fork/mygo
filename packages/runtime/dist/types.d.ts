/** The operating system, using Node.js' `process.platform` names. */
export type Platform = "darwin" | "linux" | "win32";
/** Controls for the window hosting the page, handy for custom title bars. */
export interface WindowControls {
    minimize(): Promise<void>;
    maximize(): Promise<void>;
    unmaximize(): Promise<void>;
    toggleMaximize(): Promise<void>;
    isMaximized(): Promise<boolean>;
    toggleFullScreen(): Promise<void>;
    close(): Promise<void>;
    setTitle(title: string): Promise<void>;
}
/** The runtime MyGo injects into every page as `window.mygo`. */
export interface Runtime {
    /** Calls a bound Go method, e.g. `call("Greeter.Greet", "Ada")`. */
    call<T = unknown>(method: string, ...args: unknown[]): Promise<T>;
    /** Subscribes to a Go event. Returns a function that unsubscribes. */
    on<T = unknown>(event: string, listener: (payload: T) => void): () => void;
    /** Like `on`, but unsubscribes after the first event. */
    once<T = unknown>(event: string, listener: (payload: T) => void): () => void;
    /** The operating system. */
    readonly platform: Platform;
    /** Id of the Go `*mygo.Window` hosting this page. */
    readonly windowId: number;
    /** MyGo version. */
    readonly version: string;
    /** The window hosting this page. */
    readonly window: WindowControls;
}

/** The operating system, using Node.js' `process.platform` names. */
export type Platform = "darwin" | "linux" | "win32";

/** The direction a language is written in: left to right, or right to left. */
export type Direction = "ltr" | "rtl";

/** The payload of the `mygo:locale` event. */
export interface LocaleChange {
  /** The language the app shows now, e.g. "zh-Hans". */
  locale: string;
  /** The direction it is written in. */
  dir: Direction;
}

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

/**
 * A stream of values from a Go method that takes a `*mygo.Channel[T]`,
 * passed to the call in its place. Iterate it, or handle the values with
 * `onmessage`: values that arrive before either wait for it. The channel
 * closes when the method returns, or calls `Close`, once its values are
 * taken; `close()` stops the method.
 */
export interface Channel<T = unknown> extends AsyncIterable<T> {
  /** Called with each value, instead of iterating. */
  onmessage: ((value: T) => void) | null;
  /** Called when the channel closes. */
  onclose: (() => void) | null;
  /** Whether the channel is closed. */
  readonly closed: boolean;
  /**
   * Closes the channel, dropping values not taken yet: the Go method's
   * `Send` fails and its context is canceled. Breaking out of a
   * `for await` loop over the channel closes it too.
   */
  close(): void;
}

/** The runtime MyGo injects into every page as `window.mygo`. */
export interface Runtime {
  /** Calls a bound Go method, e.g. `call("Greeter.Greet", "Ada")`. */
  call<T = unknown>(method: string, ...args: unknown[]): Promise<T>;
  /**
   * Creates a channel to pass to a call of a Go method that streams values
   * through a `*mygo.Channel[T]` parameter. A channel serves one call.
   */
  channel<T = unknown>(onmessage?: (value: T) => void): Channel<T>;
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
  /**
   * The language the app shows, a BCP 47 tag such as "en" or "zh-Hans"
   * (`App.Locale`): of the languages the app is written in, the one the
   * user reads best, or the one `App.SetLocale` chose. A page has it from
   * document start, and the `mygo:locale` event tells of changes.
   */
  readonly locale: string;
  /** The direction `locale` is written in, for `<html dir>`. */
  readonly dir: Direction;
  /** The window hosting this page. */
  readonly window: WindowControls;
}

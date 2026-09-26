import type { BridgeConfig, Incoming, Outgoing, Runtime } from "./types";

type Pending = { method: string; resolve: (v: any) => void; reject: (e: unknown) => void };
type Listener = (payload: any) => void;

/** Internal API called by the Go side through `window.__mygo`. */
export interface Internal {
  /** Delivers one or more messages from Go. */
  receive(messages: Incoming | Incoming[]): void;
}

/** Error thrown by `call` when the Go method returns an error. */
export class CallError extends Error {
  constructor(
    readonly method: string,
    message: string,
  ) {
    super(message);
    this.name = "CallError";
  }
}

/**
 * Creates the renderer runtime. `post` delivers a serialized message to the
 * Go side. Kept free of DOM access so it can be unit tested.
 */
export function createRuntime(
  config: BridgeConfig,
  post: (message: string) => void,
): { runtime: Runtime; internal: Internal } {
  // A per-page token keeps replies addressed to a previous page (before a
  // navigation) from resolving promises of the current one.
  const token = Math.random().toString(36).slice(2) + Date.now().toString(36);
  const pending = new Map<number, Pending>();
  const listeners = new Map<string, Set<Listener>>();
  let seq = 0;

  const send = (message: Outgoing) => post(JSON.stringify(message));

  const call = <T>(method: string, ...args: unknown[]): Promise<T> => {
    const id = ++seq;
    return new Promise<T>((resolve, reject) => {
      pending.set(id, { method, resolve, reject });
      try {
        send({ t: "call", id, k: token, m: method, a: args });
      } catch (err) {
        pending.delete(id);
        reject(err);
      }
    });
  };

  const on = (event: string, listener: Listener) => {
    if (typeof listener !== "function") throw new TypeError("listener must be a function");
    let set = listeners.get(event);
    if (!set) listeners.set(event, (set = new Set()));
    // Wrap so the same function can be subscribed twice and removed once.
    const entry: Listener = (p) => listener(p);
    set.add(entry);
    return () => {
      set.delete(entry);
      if (set.size === 0 && listeners.get(event) === set) listeners.delete(event);
    };
  };

  const once = (event: string, listener: Listener) => {
    const off = on(event, (p) => {
      off();
      listener(p);
    });
    return off;
  };

  const internal: Internal = {
    receive(messages) {
      for (const msg of Array.isArray(messages) ? messages : [messages]) {
        if (msg.t === "event") {
          const set = listeners.get(msg.n);
          if (!set) continue;
          for (const listener of [...set]) {
            try {
              listener(msg.p);
            } catch (err) {
              // One failing listener must not break the others.
              setTimeout(() => {
                throw err;
              });
            }
          }
        } else if (msg.t === "reply") {
          if (msg.k !== token) continue;
          const p = pending.get(msg.id);
          if (!p) continue;
          pending.delete(msg.id);
          if (msg.ok) p.resolve(msg.v);
          else p.reject(new CallError(p.method, msg.e ?? "unknown error"));
        }
      }
    },
  };

  const win = (op: string) => () => call<any>(`mygo:window.${op}`);
  const runtime: Runtime = Object.freeze({
    call,
    on,
    once,
    platform: config.platform,
    windowId: config.windowId,
    version: config.version,
    window: Object.freeze({
      minimize: win("Minimize"),
      maximize: win("Maximize"),
      unmaximize: win("Unmaximize"),
      toggleMaximize: win("ToggleMaximize"),
      isMaximized: win("IsMaximized"),
      toggleFullScreen: win("ToggleFullScreen"),
      close: win("Close"),
      setTitle: (title: string) => call<void>("mygo:window.SetTitle", title),
    }),
  });

  return { runtime, internal };
}

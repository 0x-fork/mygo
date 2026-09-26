// The public types are those of the mygo-runtime package.
import type { Platform } from "../../runtime/src/types";

export type { Platform, Runtime, WindowControls } from "../../runtime/src/types";

/** Configuration injected by the Go side in front of the bridge. */
export interface BridgeConfig {
  platform: Platform;
  windowId: number;
  version: string;
  /** Prefixes every message, so Go can tell ours from other frames'. */
  secret: string;
}

/** Messages posted from the page to Go. */
export type Outgoing =
  | { t: "call"; id: number; k: string; m: string; a: unknown[] }
  | { t: "dom-ready" }
  | { t: "drag" }
  | { t: "dblclick" };

/** Messages delivered from Go to the page. */
export type Incoming =
  | { t: "event"; n: string; p?: unknown }
  | { t: "reply"; id: number; k: string; ok: boolean; v?: unknown; e?: string };

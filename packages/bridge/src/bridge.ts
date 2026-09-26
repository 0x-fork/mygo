// Entry point of the script MyGo injects at document start into every page.
// The Go side wraps the bundle in a function that defines __MYGO_CONFIG__.
import { createRuntime } from "./runtime";
import type { BridgeConfig } from "./types";

declare const __MYGO_CONFIG__: BridgeConfig;

// Elements that keep receiving clicks inside a drag region.
const INTERACTIVE =
  'input,textarea,select,button,a[href],summary,label,[contenteditable]:not([contenteditable="false"]),video[controls],audio[controls]';

(() => {
  const w = window as any;
  if (w.__mygo) return;

  const raw = transport(w);
  if (!raw) return;
  // Only this closure knows the secret: the handler is also reachable from
  // iframes, whose messages Go must ignore.
  const post = (m: string) => raw(__MYGO_CONFIG__.secret + m);

  const { runtime, internal } = createRuntime(__MYGO_CONFIG__, post);
  Object.defineProperty(w, "mygo", { value: runtime, enumerable: true });
  Object.defineProperty(w, "__mygo", { value: internal });

  const notify = (t: "dom-ready" | "drag" | "dblclick") => {
    try {
      post(JSON.stringify({ t }));
    } catch {
      // The page is being torn down.
    }
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => notify("dom-ready"), { once: true });
  } else {
    notify("dom-ready");
  }

  const nativeRegion = typeof CSS !== "undefined" && CSS.supports("-webkit-app-region", "drag");
  const appRegion = (el: Element): string => {
    const custom = getComputedStyle(el).getPropertyValue("--app-region").trim();
    if (custom) return custom;
    if (!nativeRegion) return "";
    // The native property is not inherited, so walk up the tree.
    for (let node: Element | null = el; node; node = node.parentElement) {
      const value = getComputedStyle(node).getPropertyValue("-webkit-app-region").trim();
      if (value === "drag" || value === "no-drag") return value;
    }
    return "";
  };

  // Frameless windows: `--app-region: drag` (or `-webkit-app-region: drag`
  // where the engine supports it) turns an element into a window handle.
  w.addEventListener(
    "mousedown",
    (e: MouseEvent) => {
      if (e.button !== 0 || !(e.target instanceof Element)) return;
      if (appRegion(e.target) !== "drag" || e.target.closest(INTERACTIVE)) return;
      e.preventDefault();
      notify(e.detail === 2 ? "dblclick" : "drag");
    },
    true,
  );
})();

function transport(w: any): ((message: string) => void) | null {
  const handler = w.webkit?.messageHandlers?.mygo;
  if (handler) return (m) => handler.postMessage(m);
  const webview = w.chrome?.webview;
  if (webview) return (m) => webview.postMessage(m);
  return null;
}

// src/index.ts
function isMyGo() {
  return typeof globalThis === "object" && globalThis.mygo !== undefined;
}
function runtime() {
  const rt = globalThis.mygo;
  if (!rt)
    throw new Error("mygo: runtime not found; is this page running inside a MyGo window?");
  return rt;
}
function call(method, ...args) {
  try {
    return runtime().call(method, ...args);
  } catch (err) {
    return Promise.reject(err);
  }
}
function on(name, listener) {
  return runtime().on(name, listener);
}
function once(name, listener) {
  return runtime().once(name, listener);
}
function event(name) {
  return {
    name,
    on: (listener) => on(name, listener),
    once: (listener) => once(name, listener)
  };
}
function isCallError(err) {
  return err instanceof Error && err.name === "CallError";
}
var currentWindow = {
  minimize: async () => runtime().window.minimize(),
  maximize: async () => runtime().window.maximize(),
  unmaximize: async () => runtime().window.unmaximize(),
  toggleMaximize: async () => runtime().window.toggleMaximize(),
  isMaximized: async () => runtime().window.isMaximized(),
  toggleFullScreen: async () => runtime().window.toggleFullScreen(),
  close: async () => runtime().window.close(),
  setTitle: async (title) => runtime().window.setTitle(title)
};
export {
  call,
  currentWindow,
  event,
  isCallError,
  isMyGo,
  on,
  once,
  runtime
};

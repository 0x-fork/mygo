# mygo-runtime

Typed access to the runtime of [MyGo](https://github.com/egoist/mygo) apps,
for their web frontends.

MyGo injects its runtime into every page of an app as `window.mygo`. This
package gives it types and imports. The client that `mygo generate` writes
for your bound Go services is built on it, so you mostly use that:

```ts
import { Greeter, events } from "./mygo"; // generated

const greeting = await Greeter.greet("Ada");
events.tick.on((time) => console.log(time));
```

Use the package directly for the window and the platform:

```ts
import { currentWindow, isCallError, isMyGo, runtime } from "mygo-runtime";

if (isMyGo() && runtime().platform === "darwin") {
  document.body.classList.add("mac");
}
await currentWindow.toggleMaximize();
```

A Go method returning an error rejects with a `CallError` (see
`isCallError`). Outside a MyGo window, e.g. when the dev server is opened in a
browser, calls reject and `isMyGo()` is false.

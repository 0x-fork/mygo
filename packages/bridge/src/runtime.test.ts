import { describe, expect, test } from "bun:test";
import { CallError, createRuntime } from "./runtime";
import type { Outgoing } from "./types";

type CallMsg = Extract<Outgoing, { t: "call" }>;

function setup() {
  const sent: Outgoing[] = [];
  const rt = createRuntime({ platform: "darwin", windowId: 7, version: "0.0.0-test", secret: "s" }, (m) =>
    sent.push(JSON.parse(m)),
  );
  return { sent, ...rt };
}

describe("call", () => {
  test("resolves with the reply for the current page", async () => {
    const { sent, runtime, internal } = setup();
    const p = runtime.call("Greeter.Greet", "bun", 1);
    const msg = sent[0] as CallMsg;
    expect(msg).toMatchObject({ t: "call", id: 1, m: "Greeter.Greet", a: ["bun", 1] });

    // A reply addressed to another page (stale token) is ignored.
    internal.receive({ t: "reply", id: 1, k: "stale", ok: true, v: "wrong" });
    internal.receive({ t: "reply", id: 1, k: msg.k, ok: true, v: "hello bun" });
    expect(await p).toBe("hello bun");
  });

  test("rejects with a CallError", async () => {
    const { sent, runtime, internal } = setup();
    const p = runtime.call("Files.Read", "/nope");
    const { id, k } = sent[0] as CallMsg;
    internal.receive([{ t: "reply", id, k, ok: false, e: "file not found" }]);
    const err = (await p.catch((e) => e)) as CallError;
    expect(err).toBeInstanceOf(CallError);
    expect(err.message).toBe("file not found");
    expect(err.method).toBe("Files.Read");
  });

  test("concurrent calls resolve independently", async () => {
    const { sent, runtime, internal } = setup();
    const a = runtime.call("A.Do");
    const b = runtime.call("B.Do");
    const [ma, mb] = sent as CallMsg[];
    internal.receive([
      { t: "reply", id: mb!.id, k: mb!.k, ok: true, v: "b" },
      { t: "reply", id: ma!.id, k: ma!.k, ok: true, v: "a" },
    ]);
    expect(await Promise.all([a, b])).toEqual(["a", "b"]);
  });

  test("window controls call built-in methods", () => {
    const { sent, runtime } = setup();
    runtime.window.minimize();
    runtime.window.setTitle("Hi");
    expect(sent.map((m) => (m as CallMsg).m)).toEqual(["mygo:window.Minimize", "mygo:window.SetTitle"]);
    expect((sent[1] as CallMsg).a).toEqual(["Hi"]);
  });
});

describe("events", () => {
  test("on / once / unsubscribe", () => {
    const { runtime, internal } = setup();
    const seen: unknown[] = [];
    const off = runtime.on("tick", (v) => seen.push(["on", v]));
    runtime.once("tick", (v) => seen.push(["once", v]));

    internal.receive({ t: "event", n: "tick", p: 1 });
    internal.receive({ t: "event", n: "tick", p: 2 });
    expect(seen).toEqual([
      ["on", 1],
      ["once", 1],
      ["on", 2],
    ]);

    off();
    internal.receive({ t: "event", n: "tick", p: 3 });
    expect(seen.length).toBe(3);
  });

  test("the same listener can subscribe twice", () => {
    const { runtime, internal } = setup();
    let n = 0;
    const fn = () => n++;
    const off1 = runtime.on("x", fn);
    runtime.on("x", fn);
    internal.receive({ t: "event", n: "x" });
    off1();
    internal.receive({ t: "event", n: "x" });
    expect(n).toBe(3);
  });
});

test("runtime object is frozen", () => {
  const { runtime } = setup();
  expect(runtime.platform).toBe("darwin");
  expect(runtime.windowId).toBe(7);
  expect(Object.isFrozen(runtime)).toBe(true);
  expect(Object.isFrozen(runtime.window)).toBe(true);
});

import { createServerFn } from "@tanstack/react-start"
import { staticFunctionMiddleware } from "@tanstack/start-static-server-functions"

import { loadBenchmarkDocs } from "@/lib/benchmarks.server"
import { site } from "@/lib/site"

/**
 * latest.json of the benchmarks branch, which scripts/bench.ts writes (its
 * Latest): the last commits measured, oldest first, and their values by
 * series.
 */
export interface BenchmarkData {
  version: 1
  commits: Commit[]
  /** By OS: the runner of each commit, null where the OS has no run. */
  runners: Record<string, (Runner | null)[]>
  /** By OS, package, benchmark and unit: the value at each commit, or null. */
  series: Record<string, Record<string, Record<string, Record<string, (number | null)[]>>>>
}

export interface Commit {
  sha: string
  date: string
  message: string
}

export interface Runner {
  arch: string
  cpu: string
  go: string
}

/** Where the page reads the results: the browser fetches them, as the workflow pushes them after the site is built. */
export const dataUrl: string = import.meta.env.VITE_BENCHMARKS_URL || "https://raw.githubusercontent.com/egoist/mygo/benchmarks/latest.json"

export const workflowUrl = `${site.repo}/actions/workflows/bench.yml`

export const getBenchmarkDocs = createServerFn({ method: "GET" })
  .middleware([staticFunctionMiddleware])
  .handler(() => loadBenchmarkDocs())

export const platforms = [
  { id: "darwin", label: "macOS" },
  { id: "linux", label: "Linux" },
  { id: "windows", label: "Windows" },
] as const

export type Platform = (typeof platforms)[number]["id"]

export const metrics = [
  { id: "time", unit: "ns/op", label: "Time", text: "time per operation" },
  { id: "memory", unit: "B/op", label: "Memory", text: "memory allocated per operation" },
  { id: "allocs", unit: "allocs/op", label: "Allocations", text: "allocations per operation" },
] as const

export type Metric = (typeof metrics)[number]["id"]

export const ranges = [50, 100, 300] as const

export interface Group {
  /** The package's directory, as scripts/bench.ts names it. */
  pkg: string
  title: string
  text: string
  /** The unit its charts show whatever the metric chosen, for results that are not Go benchmarks'. */
  unit?: string
}

export const groups: Group[] = [
  {
    pkg: "size",
    title: "App size",
    text: "Release builds of two examples, compiled as mygo build compiles them, without their icons.",
    unit: "bytes",
  },
  {
    pkg: "internal/e2e",
    title: "Webview and windows",
    text: "The real backend: pages calling Go through the system webview, streaming from it, receiving its events and fetching from the app's scheme, and windows opening.",
  },
  {
    pkg: ".",
    title: "IPC, Go side",
    text: "What MyGo itself spends on calls, channels, events and custom schemes: decoding, calling, encoding and batching, on the fake backend, without a webview.",
  },
  { pkg: "ui", title: "Native UI", text: "Frames of native UI built, laid out, painted and rendered on the CPU; text areas; lists." },
  { pkg: "internal/text", title: "Text layout", text: "Paragraphs and labels shaped and broken into lines." },
  { pkg: "internal/raster", title: "CPU renderer", text: "Scenes drawn into memory, as windows without a GPU draw them." },
  { pkg: "internal/svg", title: "SVG", text: "Icons parsed and drawn." },
  { pkg: "plugins/terminal", title: "Terminal", text: "The terminal plugin taking a program's output, and drawing its view." },
]

/** What the sizes measure, which are not Go benchmarks with comments. */
export const appDocs: Record<string, string> = {
  "size/hello": "examples/hello: a window with a web page calling a Go method.",
  "size/counter-native": "examples/counter-native: a window of native UI.",
}

/** The smallest change of a series that stands out, whatever its noise. */
function minChange(unit: string) {
  return unit === "ns/op" ? 0.05 : unit === "bytes" ? 0.002 : 0.01
}

export interface SeriesStats {
  /** The index of the last value. */
  index: number
  latest: number
  /** The median of the five values before the last. */
  baseline?: number
  /** latest / baseline - 1. */
  change?: number
  /** How much the series moves on its own, relatively: the change that stands out. */
  noise: number
  significant: boolean
}

/** Compares a series' last value with the ones before it, whose spread tells its noise. */
export function seriesStats(values: (number | null)[], unit: string): SeriesStats | undefined {
  let index = values.length - 1
  while (index >= 0 && values[index] == null) index--
  if (index < 0) return undefined
  const latest = values[index]!
  const before = values.slice(0, index).filter((v): v is number => v != null)
  const recent = before.slice(-20)
  let noise = minChange(unit)
  if (recent.length >= 5) {
    const m = median(recent)
    // Five median absolute deviations, a robust spread: about one series
    // in a thousand moves that much by chance.
    if (m > 0) noise = Math.max(noise, (5 * median(recent.map((v) => Math.abs(v - m)))) / m)
  }
  const last = before.slice(-5)
  if (!last.length) return { index, latest, noise, significant: false }
  const baseline = median(last)
  const change = baseline > 0 ? latest / baseline - 1 : latest === 0 ? 0 : undefined
  return { index, latest, baseline, change, noise, significant: change !== undefined && Math.abs(change) > noise }
}

export function median(values: number[]) {
  const s = [...values].sort((a, b) => a - b)
  const mid = s.length >> 1
  return s.length % 2 ? s[mid]! : (s[mid - 1]! + s[mid]!) / 2
}

function sig(v: number) {
  return v >= 100 ? Math.round(v).toLocaleString("en-US") : String(Number(v.toPrecision(3)))
}

/** Formats a value of a unit of scripts/bench.ts: "27.4 µs", "4.09 kB", "14 allocs". */
export function formatValue(v: number, unit: string) {
  switch (unit) {
    case "ns/op":
      if (v >= 1e9) return `${sig(v / 1e9)} s`
      if (v >= 1e6) return `${sig(v / 1e6)} ms`
      if (v >= 1e3) return `${sig(v / 1e3)} µs`
      return `${sig(v)} ns`
    case "B/op":
    case "bytes":
      if (v >= 1e9) return `${sig(v / 1e9)} GB`
      if (v >= 1e6) return `${sig(v / 1e6)} MB`
      if (v >= 1e3) return `${sig(v / 1e3)} kB`
      return `${sig(v)} B`
    case "allocs/op":
      return `${Number.isInteger(v) ? v.toLocaleString("en-US") : sig(v)} ${v === 1 ? "alloc" : "allocs"}`
    default:
      return `${sig(v)} ${unit}`
  }
}

export function formatChange(change: number) {
  const pct = Math.abs(change * 100)
  const digits = pct < 10 ? 1 : 0
  return `${change < 0 ? "−" : "+"}${pct.toFixed(digits)}%`
}

export function formatDate(iso: string, withYear = false) {
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric", ...(withYear ? { year: "numeric" } : {}) })
}

export function commitUrl(sha: string) {
  return `${site.repo}/commit/${sha}`
}

/** The id of a benchmark's chart, as a fragment of the page's URL. */
export function anchorOf(pkg: string, name: string) {
  return `${pkg === "." ? "mygo" : pkg}/${name}`.replace(/[^\w-]+/g, "-")
}

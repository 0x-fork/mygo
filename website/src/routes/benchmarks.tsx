import { Link, createFileRoute } from "@tanstack/react-router"
import { ArrowDownIcon, ArrowUpIcon } from "lucide-react"
import * as React from "react"

import { TrendChart } from "@/components/benchmarks/trend-chart"
import {
  anchorOf,
  appDocs,
  commitUrl,
  dataUrl,
  formatChange,
  formatDate,
  formatValue,
  getBenchmarkDocs,
  groups,
  metrics,
  platforms,
  ranges,
  seriesStats,
  workflowUrl,
  type BenchmarkData,
  type Group,
  type Metric,
  type Platform,
  type SeriesStats,
} from "@/lib/benchmarks"
import { site } from "@/lib/site"
import { cn } from "@/lib/utils"

const title = "Benchmarks · MyGo"
const description = "MyGo's benchmarks on macOS, Linux and Windows, measured on every push to main: IPC through the webview, windows opening, native UI frames, text layout and app size."

export const Route = createFileRoute("/benchmarks")({
  loader: () => getBenchmarkDocs(),
  staleTime: Infinity,
  head: () => ({
    meta: [
      { title },
      { name: "description", content: description },
      { property: "og:title", content: title },
      { property: "og:description", content: description },
    ],
  }),
  component: Benchmarks,
})

type Load = { state: "loading" } | { state: "missing" } | { state: "error"; error: string } | { state: "ready"; data: BenchmarkData }

interface View {
  os: Platform
  metric: Metric
  range: number
}

const defaultView: View = { os: "darwin", metric: "time", range: 100 }

/** The view the page's URL asks for, such as ?os=linux&metric=allocs&commits=300. */
function viewOf(search: string): View {
  const q = new URLSearchParams(search)
  const os = platforms.find((p) => p.id === q.get("os"))?.id ?? defaultView.os
  const metric = metrics.find((m) => m.id === q.get("metric"))?.id ?? defaultView.metric
  const range = ranges.find((r) => String(r) === q.get("commits")) ?? defaultView.range
  return { os, metric, range }
}

function Benchmarks() {
  const docs = Route.useLoaderData()
  const [load, setLoad] = React.useState<Load>({ state: "loading" })
  const [view, setView] = React.useState(defaultView)

  // The results come from the benchmarks branch, newer than the page.
  React.useEffect(() => {
    setView(viewOf(window.location.search))
    const controller = new AbortController()
    fetch(dataUrl, { signal: controller.signal })
      .then(async (res) => {
        // Until the workflow's first run, the branch has no results.
        if (res.status === 404) return setLoad({ state: "missing" })
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        setLoad({ state: "ready", data: (await res.json()) as BenchmarkData })
      })
      .catch((err: Error) => !controller.signal.aborted && setLoad({ state: "error", error: err.message }))
    return () => controller.abort()
  }, [])

  const update = (change: Partial<View>) => {
    const next = { ...view, ...change }
    setView(next)
    const q = new URLSearchParams()
    if (next.os !== defaultView.os) q.set("os", next.os)
    if (next.metric !== defaultView.metric) q.set("metric", next.metric)
    if (next.range !== defaultView.range) q.set("commits", String(next.range))
    const search = q.size ? `?${q}` : ""
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${search}${window.location.hash}`)
  }

  return (
    <main className="flex-1">
      <section className="px-4 pt-16 pb-12 sm:px-10 md:pt-20">
        <p className="label">Benchmarks</p>
        <h1 className="mt-5 text-3xl leading-[1.1] font-semibold tracking-[-0.035em] sm:text-4xl">Every push, measured.</h1>
        <p className="mt-5 max-w-2xl text-base leading-7 text-muted-foreground">
          MyGo&apos;s benchmarks run on every push to main, on GitHub&apos;s macOS, Linux and Windows runners. Each point is a commit, the median of six
          runs, and lower is better on every chart. Runners are shared virtual machines: timings drift a few percent from run to run, and jump when a
          runner gets another CPU, which a vertical line marks.
        </p>
        <p className="mt-6 flex flex-wrap gap-x-6 gap-y-2 text-sm">
          <a href={workflowUrl} className="font-medium underline decoration-gopher/45 underline-offset-[5px] hover:decoration-gopher">
            The workflow
          </a>
          <a href={`${site.repo}/tree/benchmarks`} className="font-medium underline decoration-gopher/45 underline-offset-[5px] hover:decoration-gopher">
            The data
          </a>
          <Link
            to="/docs/$"
            params={{ _splat: "architecture" }}
            hash="benchmarks"
            className="font-medium underline decoration-gopher/45 underline-offset-[5px] hover:decoration-gopher"
          >
            Running them yourself
          </Link>
        </p>
      </section>

      {/* Sticky where the controls fit on a line. */}
      <div className="z-30 flex flex-wrap items-center gap-x-8 gap-y-3 border-y bg-background/85 px-4 py-3 backdrop-blur-xl sm:px-10 lg:sticky lg:top-14">
        <Segmented label="Platform" options={platforms} value={view.os} onChange={(os) => update({ os })} />
        <Segmented label="Metric" options={metrics} value={view.metric} onChange={(metric) => update({ metric })} />
        <Segmented
          label="Commits"
          options={ranges.map((r) => ({ id: r, label: String(r) }))}
          value={view.range}
          onChange={(range) => update({ range })}
        />
      </div>

      {load.state === "loading" && <Message>Loading the results…</Message>}
      {load.state === "missing" && (
        <Message>
          No results yet: they show once{" "}
          <a href={workflowUrl} className="text-foreground underline underline-offset-4">
            the workflow
          </a>{" "}
          has measured a commit.
        </Message>
      )}
      {load.state === "error" && (
        <Message>
          The results could not be loaded ({load.error}). They are at{" "}
          <a href={dataUrl} className="text-foreground underline underline-offset-4">
            latest.json
          </a>
          .
        </Message>
      )}
      {load.state === "ready" && <Results data={load.data} docs={docs} view={view} onMetric={(metric) => update({ metric })} />}
    </main>
  )
}

function Results({
  data,
  docs,
  view,
  onMetric,
}: {
  data: BenchmarkData
  docs: Record<string, string>
  view: View
  onMetric: (metric: Metric) => void
}) {
  const platform = platforms.find((p) => p.id === view.os)!
  const series = data.series[view.os]
  const runners = data.runners[view.os] ?? []
  if (!series) return <Message>No results on {platform.label} yet.</Message>

  const from = Math.max(0, data.commits.length - view.range)
  const commits = data.commits.slice(from)
  const windowRunners = runners.slice(from)
  const unit = metrics.find((m) => m.id === view.metric)!.unit
  const known = new Set(groups.map((g) => g.pkg))
  const shown: Group[] = [...groups, ...Object.keys(series).filter((pkg) => !known.has(pkg)).map((pkg) => ({ pkg, title: pkg, text: "" }))]

  let last = runners.length - 1
  while (last >= 0 && !runners[last]) last--
  const latest = data.commits[last]
  const runner = runners[last]

  // What moved in the last commit measured, beyond each series' noise.
  const moved: { pkg: string; name: string; metric: (typeof metrics)[number] | undefined; unit: string; stats: SeriesStats }[] = []
  for (const [pkg, benchmarks] of Object.entries(series)) {
    for (const [name, units] of Object.entries(benchmarks)) {
      for (const [u, values] of Object.entries(units)) {
        const stats = seriesStats(values, u)
        if (stats?.significant && stats.index === last) moved.push({ pkg, name, metric: metrics.find((m) => m.unit === u), unit: u, stats })
      }
    }
  }
  moved.sort((a, b) => Number(b.stats.change! > 0) - Number(a.stats.change! > 0) || Math.abs(b.stats.change!) - Math.abs(a.stats.change!))

  const jump = (pkg: string, name: string, metric?: Metric) => {
    if (metric) onMetric(metric)
    requestAnimationFrame(() => document.getElementById(anchorOf(pkg, name))?.scrollIntoView({ block: "start" }))
  }

  return (
    <>
      {latest && runner && (
        <section className="grid border-b md:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)] md:divide-x">
          <div className="px-4 py-8 sm:px-10">
            <p className="label">Last measured on {platform.label}</p>
            <a href={commitUrl(latest.sha)} className="group mt-3 block">
              <span className="font-mono text-sm text-gopher-ink">{latest.sha.slice(0, 7)}</span>{" "}
              <span className="font-medium group-hover:underline group-hover:underline-offset-4">{latest.message}</span>
            </a>
            <p className="mt-2 text-sm text-muted-foreground">
              {formatDate(latest.date, true)} · {runner.cpu} · {runner.go}
            </p>
          </div>
          <div className="border-t px-4 py-8 sm:px-10 md:border-t-0">
            <p className="label">Moved beyond noise</p>
            {moved.length ? (
              <ul className="mt-3 flex flex-wrap gap-2">
                {moved.slice(0, 8).map((m) => (
                  <li key={`${m.pkg}/${m.name}/${m.unit}`}>
                    <button
                      type="button"
                      onClick={() => jump(m.pkg, m.name, m.metric?.id)}
                      className="inline-flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-sm transition-colors hover:bg-muted"
                    >
                      <Change stats={m.stats} />
                      <span className="font-mono text-[13px]">{displayName(m.pkg, m.name)}</span>
                      <span className="text-muted-foreground">{m.metric?.label.toLowerCase() ?? m.unit}</span>
                    </button>
                  </li>
                ))}
                {moved.length > 8 && <li className="self-center text-sm text-muted-foreground">and {moved.length - 8} more</li>}
              </ul>
            ) : (
              <p className="mt-3 text-sm text-muted-foreground">Nothing: every result is within the noise of the commits before.</p>
            )}
          </div>
        </section>
      )}

      {shown.map((group) => {
        const benchmarks = series[group.pkg]
        const u = group.unit ?? unit
        const rows = Object.entries(benchmarks ?? {}).filter(([, units]) => units[u]?.some((v) => v != null))
        if (!rows.length) return null
        return (
          <GroupSection key={group.pkg} group={group} unit={u}>
            {(table) =>
              table ? (
                <ResultsTable pkg={group.pkg} rows={rows.map(([name, units]) => [name, units[u]!])} unit={u} />
              ) : (
                <div className="-mr-px -mb-px grid border-t sm:grid-cols-2 lg:grid-cols-3">
                  {rows.map(([name, units]) => {
                    const values = units[u]!
                    const stats = seriesStats(values, u)
                    const doc = docs[`${group.pkg}/${name.split("/")[0]}`] ?? appDocs[`${group.pkg}/${name}`]
                    const label = `${displayName(group.pkg, name)}: ${stats ? formatValue(stats.latest, u) : "no results"}`
                    return (
                      <article key={name} id={anchorOf(group.pkg, name)} className="flex min-w-0 scroll-mt-16 flex-col lg:scroll-mt-32 gap-4 border-r border-b px-4 py-5 sm:px-6">
                        <header className="flex items-start justify-between gap-4">
                          <div className="min-w-0">
                            <h3 className="truncate font-mono text-[13px] font-medium" title={name}>
                              {name}
                            </h3>
                            {doc && (
                              <p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground" title={doc}>
                                {doc}
                              </p>
                            )}
                          </div>
                          {stats && (
                            <div className="shrink-0 text-right">
                              <div className="text-lg leading-6 font-semibold tracking-tight">{formatValue(stats.latest, u)}</div>
                              <Change stats={stats} className="text-xs" />
                            </div>
                          )}
                        </header>
                        <TrendChart values={values.slice(from)} commits={commits} runners={windowRunners} unit={u} label={label} />
                      </article>
                    )
                  })}
                </div>
              )
            }
          </GroupSection>
        )
      })}
    </>
  )
}

function GroupSection({ group, unit, children }: { group: Group; unit: string; children: (table: boolean) => React.ReactNode }) {
  const [table, setTable] = React.useState(false)
  const id = `group-${anchorOf(group.pkg, "")}`
  return (
    <section aria-labelledby={id} className="border-b">
      <div className="flex flex-wrap items-end justify-between gap-4 px-4 pt-12 pb-6 sm:px-10">
        <div className="max-w-2xl">
          <h2 id={id} className="text-xl font-semibold tracking-tight">
            {group.title}
          </h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            {group.text}
            {group.text && " "}
            <span className="whitespace-nowrap">In {metrics.find((m) => m.unit === unit)?.text ?? unit}.</span>
          </p>
        </div>
        <Segmented
          label="View"
          hideLabel
          options={[
            { id: false, label: "Charts" },
            { id: true, label: "Table" },
          ]}
          value={table}
          onChange={setTable}
        />
      </div>
      {children(table)}
    </section>
  )
}

/** The table view of a group: each benchmark's last value against the five before. */
function ResultsTable({ pkg, rows, unit }: { pkg: string; rows: [string, (number | null)[]][]; unit: string }) {
  const th = "border-b px-4 py-2.5 font-mono text-[11px] font-medium tracking-[0.08em] whitespace-nowrap text-muted-foreground uppercase sm:px-6"
  const td = "border-b px-4 py-2.5 whitespace-nowrap tabular-nums sm:px-6"
  return (
    <div className="overflow-x-auto border-t">
      <table className="w-full border-collapse text-left text-sm">
        <thead>
          <tr>
            <th className={th}>Benchmark</th>
            <th className={cn(th, "text-right")}>Last</th>
            <th className={cn(th, "text-right")}>Median of the 5 before</th>
            <th className={cn(th, "text-right")}>Change</th>
            <th className={cn(th, "text-right")}>Noise</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([name, values]) => {
            const stats = seriesStats(values, unit)
            return (
              <tr key={name} id={`${anchorOf(pkg, name)}-row`}>
                <td className={cn(td, "font-mono text-[13px]")}>{name}</td>
                <td className={cn(td, "text-right font-medium")}>{stats ? formatValue(stats.latest, unit) : "—"}</td>
                <td className={cn(td, "text-right text-muted-foreground")}>{stats?.baseline !== undefined ? formatValue(stats.baseline, unit) : "—"}</td>
                <td className={cn(td, "text-right")}>{stats ? <Change stats={stats} /> : "—"}</td>
                <td className={cn(td, "text-right text-muted-foreground")}>{stats ? `±${(stats.noise * 100).toFixed(1)}%` : "—"}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/** The change of a series' last value: worse (up) or better (down) beyond its noise, with an arrow, or muted within it. */
function Change({ stats, className }: { stats: SeriesStats; className?: string }) {
  if (stats.change === undefined) return <span className={cn("text-muted-foreground", className)}>new</span>
  const text = formatChange(stats.change)
  const hint = `Against the median of the 5 commits before; this series moves ±${(stats.noise * 100).toFixed(1)}% on its own`
  if (!stats.significant) {
    return (
      <span className={cn("text-muted-foreground tabular-nums", className)} title={hint}>
        {text}
      </span>
    )
  }
  const worse = stats.change > 0
  const Icon = worse ? ArrowUpIcon : ArrowDownIcon
  return (
    <span className={cn("inline-flex items-center gap-0.5 font-medium tabular-nums", worse ? "text-worse" : "text-better", className)} title={hint}>
      <Icon className="size-3.5" aria-hidden />
      {text}
      <span className="sr-only">{worse ? " worse" : " better"}</span>
    </span>
  )
}

function Segmented<T extends string | number | boolean>({
  label,
  hideLabel,
  options,
  value,
  onChange,
}: {
  label: string
  hideLabel?: boolean
  options: readonly { id: T; label: string }[]
  value: T
  onChange: (value: T) => void
}) {
  return (
    <div className="flex items-center gap-3">
      {!hideLabel && <span className="label">{label}</span>}
      <div role="radiogroup" aria-label={label} className="inline-flex rounded-lg border p-0.5">
        {options.map((o) => (
          <button
            key={String(o.id)}
            type="button"
            role="radio"
            aria-checked={o.id === value}
            onClick={() => onChange(o.id)}
            className={cn(
              "h-7 rounded-md px-2.5 text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
              o.id === value ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:text-foreground"
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  )
}

function Message({ children }: { children: React.ReactNode }) {
  return <p className="px-4 py-24 text-center text-sm text-muted-foreground sm:px-10">{children}</p>
}

/** A benchmark as the summary names it: its package, then its name. */
function displayName(pkg: string, name: string) {
  return pkg === "." ? name : `${pkg.split("/").at(-1)}/${name}`
}

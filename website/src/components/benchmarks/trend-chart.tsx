import * as React from "react"

import { commitUrl, formatDate, formatValue, median, type Commit, type Runner } from "@/lib/benchmarks"

const height = 112
const top = 16
const bottom = 6
// Room for the end dot at both ends.
const pad = 6

/**
 * A line of a benchmark's values over commits. Its axis fits the values but
 * spans at least a fifth of their median, so that noise stays small and a
 * step of a few percent shows. A vertical hairline marks a commit measured
 * on another CPU than the one before it, which moves timings on its own.
 * The crosshair follows the pointer or the arrow keys; a click or Enter
 * opens the commit.
 */
export function TrendChart({
  values,
  commits,
  runners,
  unit,
  label,
}: {
  values: (number | null)[]
  commits: Commit[]
  runners: (Runner | null)[]
  unit: string
  label: string
}) {
  const ref = React.useRef<HTMLDivElement>(null)
  const width = useWidth(ref)
  const [hover, setHover] = React.useState<number | null>(null)
  // A tap shows a commit, and a second tap on it opens it.
  const touched = React.useRef(false)

  const n = values.length
  const present = values.flatMap((v, i) => (v == null ? [] : [i]))
  const [lo, hi] = domain(present.map((i) => values[i]!))
  const step = n > 1 ? (width - 2 * pad) / (n - 1) : 0
  const x = (i: number) => (n > 1 ? pad + i * step : width / 2)
  const y = (v: number) => top + (1 - (v - lo) / (hi - lo)) * (height - top - bottom)
  const base = height - bottom

  // The line breaks where a commit has no value.
  const segments: number[][] = []
  values.forEach((v, i) => {
    if (v == null) return
    const last = segments.at(-1)
    if (last && last.at(-1) === i - 1) last.push(i)
    else segments.push([i])
  })
  const line = (seg: number[]) => seg.map((i, k) => `${k ? "L" : "M"}${x(i).toFixed(1)} ${y(values[i]!).toFixed(1)}`).join("")

  const changes: number[] = []
  let cpu: string | undefined
  runners.forEach((r, i) => {
    if (!r) return
    if (cpu !== undefined && r.cpu !== cpu) changes.push(i)
    cpu = r.cpu
  })

  /** The commit with a value nearest to index i. */
  const nearest = (i: number) => {
    let best = present[0]
    for (const p of present) if (Math.abs(p - i) < Math.abs(best! - i)) best = p
    return best ?? null
  }
  const pointAt = (clientX: number) => {
    const rect = ref.current!.getBoundingClientRect()
    return nearest(step ? Math.round((clientX - rect.left - pad) / step) : 0)
  }
  const open = (i: number | null) => {
    if (i != null && commits[i]) window.open(commitUrl(commits[i].sha), "_blank", "noopener,noreferrer")
  }
  const onKeyDown = (e: React.KeyboardEvent) => {
    const at = hover ?? present.at(-1) ?? null
    if (at == null) return
    const k = present.indexOf(at)
    const to = { ArrowLeft: present[k - 1], ArrowRight: present[k + 1], Home: present[0], End: present.at(-1) }[e.key]
    if (to !== undefined) {
      e.preventDefault()
      setHover(to)
    } else if (e.key === "Enter") open(at)
  }

  const h = hover != null && values[hover] != null ? hover : null
  const runner = h != null ? runners[h] : null
  const newRunner = h != null && changes.includes(h)

  return (
    <div ref={ref} className="relative">
      <svg
        width={width}
        height={height}
        role="img"
        aria-label={label}
        tabIndex={0}
        className="block cursor-pointer touch-pan-y rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
        onPointerDown={(e) => (touched.current = e.pointerType !== "mouse")}
        onPointerMove={(e) => setHover(pointAt(e.clientX))}
        onPointerLeave={(e) => e.pointerType === "mouse" && setHover(null)}
        onClick={(e) => {
          const i = pointAt(e.clientX)
          if (touched.current && i !== hover) setHover(i)
          else open(i)
        }}
        onFocus={() => setHover(present.at(-1) ?? null)}
        onBlur={() => setHover(null)}
        onKeyDown={onKeyDown}
      >
        {[lo, (lo + hi) / 2, hi].map((v, k) => (
          <g key={k}>
            <line x1={0} x2={width} y1={y(v)} y2={y(v)} className="stroke-border" strokeWidth={1} />
            {k !== 1 && (
              <text x={0} y={y(v) - 4} className="fill-muted-foreground text-[10px] tabular-nums">
                {formatValue(v, unit)}
              </text>
            )}
          </g>
        ))}
        {changes.map((i) => (
          <line key={i} x1={x(i)} x2={x(i)} y1={top} y2={base} className="stroke-muted-foreground/35" strokeWidth={1} />
        ))}
        {segments.map((seg) =>
          seg.length > 1 ? (
            <path key={seg[0]} d={line(seg)} fill="none" className="stroke-gopher-ink" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          ) : (
            <circle key={seg[0]} cx={x(seg[0]!)} cy={y(values[seg[0]!]!)} r={2} className="fill-gopher-ink" />
          )
        )}
        {present.length > 0 && h == null && <Dot cx={x(present.at(-1)!)} cy={y(values[present.at(-1)!]!)} />}
        {h != null && (
          <>
            <line x1={x(h)} x2={x(h)} y1={top} y2={base} className="stroke-foreground/40" strokeWidth={1} />
            <Dot cx={x(h)} cy={y(values[h]!)} />
          </>
        )}
      </svg>
      <div className="mt-1.5 flex justify-between text-[10px] text-muted-foreground tabular-nums">
        <span>{commits[0] && formatDate(commits[0].date)}</span>
        <span>{commits.at(-1) && formatDate(commits.at(-1)!.date)}</span>
      </div>
      {h != null && commits[h] && (
        <div
          className="pointer-events-none absolute top-0 z-10 w-max max-w-[min(17rem,80%)] rounded-md border bg-popover px-2.5 py-2 text-xs leading-5 text-popover-foreground shadow-md"
          style={x(h) < width / 2 ? { left: x(h) + 10 } : { right: width - x(h) + 10 }}
        >
          <div className="text-sm font-semibold tabular-nums">{formatValue(values[h]!, unit)}</div>
          <div className="truncate text-muted-foreground">
            <span className="font-mono text-foreground">{commits[h].sha.slice(0, 7)}</span> {commits[h].message}
          </div>
          <div className="text-muted-foreground">
            {formatDate(commits[h].date, true)}
            {runner && ` · ${newRunner ? "new CPU: " : ""}${runner.cpu}`}
          </div>
        </div>
      )}
    </div>
  )
}

/** The end dot, ringed with the surface. */
function Dot({ cx, cy }: { cx: number; cy: number }) {
  return <circle cx={cx} cy={cy} r={4} className="fill-gopher-ink stroke-background" strokeWidth={2} />
}

/** The axis of values: their range with some room, a fifth of their median at least, and not below zero. */
function domain(values: number[]): [number, number] {
  if (!values.length) return [0, 1]
  let lo = Math.min(...values)
  let hi = Math.max(...values)
  const mid = median(values)
  const span = Math.max((hi - lo) * 1.2, mid * 0.2) || 1
  const center = (lo + hi) / 2
  lo = Math.max(0, center - span / 2)
  hi = lo + span
  return [lo, hi]
}

function useWidth(ref: React.RefObject<HTMLElement | null>) {
  const [width, setWidth] = React.useState(320)
  React.useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.clientWidth)
    const observer = new ResizeObserver(([entry]) => entry && setWidth(entry.contentRect.width))
    observer.observe(el)
    return () => observer.disconnect()
  }, [ref])
  return width
}

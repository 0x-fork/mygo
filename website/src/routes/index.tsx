import { Link, createFileRoute } from "@tanstack/react-router"
import { ArrowRightIcon } from "lucide-react"

import { InstallCommand } from "@/components/install-command"
import { buttonVariants } from "@/components/ui/button"
import { getHome } from "@/lib/home"
import { site } from "@/lib/site"

export const Route = createFileRoute("/")({
  loader: () => getHome(),
  component: Home,
})

// From the architecture guide: a hello-world app on macOS, its memory with
// the processes WKWebView runs for it.
const stats = [
  { value: "~7", unit: "MB", label: "Binary" },
  { value: "~62", unit: "MB", label: "Memory, webview included" },
  { value: "0", unit: "%", label: "CPU at idle" },
]

// README.md's list, in its words.
const features = [
  { title: "Pure Go, no cgo", text: "Build for every platform from any machine.", slug: "distribution" },
  {
    title: "Typed IPC",
    text: "Bind Go services, stream values through channels and send typed events. The TypeScript client is generated from your Go code.",
    slug: "bindings",
  },
  {
    title: "Desktop APIs",
    text: "Windows, menus, tray, dialogs, notifications, global shortcuts, deep links, file associations and more.",
    slug: "native",
  },
  {
    title: "Ready to ship",
    text: "App bundles and disk images, Windows installers, Debian packages, code signing, notarization and signed auto-updates.",
    slug: "distribution",
  },
]

function Home() {
  const { go, ts } = Route.useLoaderData()
  return (
    <main>
      <section className="px-4 py-20 sm:px-10 md:py-28">
        <a href={`${site.repo}/releases/tag/v${__MYGO_VERSION__}`} className="label inline-flex items-center gap-2 hover:text-foreground">
          <span className="size-1.5 rounded-full bg-gopher" />v{__MYGO_VERSION__} · macOS, Linux, Windows
        </a>
        <h1 className="mt-6 text-4xl leading-[1.05] font-semibold tracking-[-0.04em] sm:text-5xl md:text-6xl">
          Desktop apps in Go.
          <span className="block text-muted-foreground">Tiny, fast, fully typed.</span>
        </h1>
        <p className="mt-6 max-w-xl text-lg leading-8 text-muted-foreground">
          One small binary for macOS, Windows and Linux, cross-compiled from any machine. Type-safe calls from TypeScript to Go, installers and auto-updates
          included.
        </p>
        <div className="mt-10 flex flex-wrap items-center gap-3">
          <InstallCommand />
          <Link to="/docs/$" params={{ _splat: "getting-started" }} className={buttonVariants({ size: "lg", className: "h-11 px-5" })}>
            Get started <ArrowRightIcon />
          </Link>
        </div>
      </section>

      <dl className="grid grid-cols-3 divide-x border-t">
        {stats.map((stat) => (
          <div key={stat.label} className="flex flex-col-reverse justify-end gap-2 px-4 py-6 sm:px-10 sm:py-8">
            <dt className="label">{stat.label}</dt>
            <dd className="text-3xl font-semibold tracking-tight tabular-nums sm:text-4xl">
              {stat.value}
              <span className="ml-1 text-base font-normal text-muted-foreground">{stat.unit}</span>
            </dd>
          </div>
        ))}
      </dl>

      <div className="grid border-t bg-code lg:grid-cols-[1.3fr_1fr] lg:divide-x">
        <figure className="min-w-0">
          <figcaption className="label border-b px-4 py-3 sm:px-6">main.go</figcaption>
          <div className="overflow-x-auto px-4 py-5 sm:px-6" dangerouslySetInnerHTML={{ __html: go }} />
        </figure>
        <figure className="flex min-w-0 flex-col border-t lg:border-t-0">
          <figcaption className="label border-b px-4 py-3 sm:px-6">src/main.ts</figcaption>
          <div className="overflow-x-auto px-4 py-5 sm:px-6" dangerouslySetInnerHTML={{ __html: ts }} />
          <p className="mt-auto border-t px-4 py-4 text-sm leading-6 text-muted-foreground sm:px-6">
            <code className="font-mono text-foreground">./mygo</code> is generated from main.go, with the types of your structs and the docs of your methods.
          </p>
        </figure>
      </div>

      <ul className="grid gap-px border-t bg-border sm:grid-cols-2">
        {features.map((feature) => (
          <li key={feature.title} className="bg-background">
            <Link to="/docs/$" params={{ _splat: feature.slug }} className="group flex h-full flex-col px-4 py-8 transition-colors hover:bg-code sm:px-10">
              <h2 className="flex items-center justify-between font-semibold tracking-tight">
                {feature.title}
                <ArrowRightIcon className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
              </h2>
              <p className="mt-2 max-w-md text-sm leading-6 text-muted-foreground">{feature.text}</p>
            </Link>
          </li>
        ))}
      </ul>
    </main>
  )
}

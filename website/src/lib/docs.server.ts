// The docs of the repository (../docs), read and rendered on the server. The
// site is prerendered, so this runs at build time only.

import fs from "node:fs/promises"
import path from "node:path"
import type { List, Root } from "mdast"
import { toString } from "mdast-util-to-string"
import remarkParse from "remark-parse"
import { unified } from "unified"

import type { Doc, NavItem, NavSection, SearchEntry } from "@/lib/docs"
import { renderMarkdown, slugOf, type RenderedMarkdown } from "@/lib/markdown.server"
import { site } from "@/lib/site"

// Vite runs in website/, next to the repository's docs.
const repoDir = path.resolve(process.cwd(), "..")
const docsDir = path.join(repoDir, "docs")

function fileOf(slug: string) {
  if (slug !== "" && !/^[a-z0-9-]+(\/[a-z0-9-]+)*$/.test(slug)) return undefined
  return path.join(docsDir, slug ? `${slug}.md` : "README.md")
}

// Pages are rendered once per build; in development, on every request.
const cache = new Map<string, Promise<RenderedMarkdown | undefined>>()

function render(slug: string) {
  let page = cache.get(slug)
  if (!page) {
    const file = fileOf(slug)
    page = file
      ? fs.readFile(file, "utf8").then(
          (source) => renderMarkdown(source, file, { docsDir, repoDir, repoUrl: site.repo }),
          () => undefined
        )
      : Promise.resolve(undefined)
    if (!import.meta.env.DEV) cache.set(slug, page)
  }
  return page
}

/** The sections of the sidebar: the lists of README.md, in its order, and pages it leaves out. */
export async function loadNav(): Promise<NavSection[]> {
  const readme = await fs.readFile(path.join(docsDir, "README.md"), "utf8")
  const tree = unified().use(remarkParse).parse(readme) as Root
  const sections: NavSection[] = [{ title: "Overview", items: [{ slug: "", title: "Introduction" }] }]
  let heading = ""
  for (const node of tree.children) {
    if (node.type === "heading" && node.depth === 2) heading = toString(node)
    if (node.type === "list" && heading) sections.push({ title: heading, items: navItems(node) })
  }

  const listed = new Set(sections.flatMap((s) => s.items.map((i) => i.slug)))
  const files = (await fs.readdir(docsDir, { recursive: true })).filter((f) => f.endsWith(".md")).sort()
  const rest: NavItem[] = []
  for (const file of files) {
    const slug = slugOf(file)
    if (listed.has(slug)) continue
    const page = await render(slug)
    rest.push({ slug, title: page?.title || slug })
  }
  if (rest.length) sections.push({ title: "More", items: rest })
  return sections
}

function navItems(list: List): NavItem[] {
  return list.children.flatMap((item): NavItem[] => {
    const paragraph = item.children[0]
    if (paragraph?.type !== "paragraph") return []
    const [first, ...rest] = paragraph.children
    const description = sentence(rest.map((n) => toString(n)).join(""))
    if (first?.type === "link" && first.url.endsWith(".md") && !/^[a-z]+:/.test(first.url)) {
      return [{ slug: slugOf(first.url), title: toString(first), description }]
    }
    // "The Go API: `go doc -all <package>`, …": the package on pkg.go.dev.
    const pkg = rest[0]?.type === "inlineCode" ? /^go doc (?:-all )?(\S+)$/.exec(rest[0].value)?.[1] : undefined
    if (first?.type === "text" && pkg) {
      return [{ href: `https://pkg.go.dev/${pkg}`, title: first.value.split(":")[0]!.replace(/^The /, "") }]
    }
    return []
  })
}

/** "`: install the tools, then …`" → "Install the tools, then …". */
function sentence(text: string) {
  const s = text.replace(/^[\s:]+/, "").replace(/\s+/g, " ").trim()
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export async function loadDoc(slug: string): Promise<Doc | undefined> {
  const [page, nav] = await Promise.all([render(slug), loadNav()])
  if (!page) return undefined
  const order = nav.flatMap((s) => s.items).filter((i) => i.slug !== undefined)
  const index = order.findIndex((i) => i.slug === slug)
  const item = order[index]
  const link = (i: NavItem | undefined) => (i?.slug === undefined ? undefined : { slug: i.slug, title: i.title })
  return {
    slug,
    title: page.title,
    description: item?.description || page.lead,
    summary: item?.description,
    section: nav.find((s) => s.items.includes(item!))?.title,
    html: page.html,
    toc: page.toc,
    prev: index > 0 ? link(order[index - 1]) : undefined,
    next: index >= 0 ? link(order[index + 1]) : undefined,
    editUrl: `${site.repo}/edit/main/docs/${slug || "README"}.md`,
  }
}

/** Every heading of every page, with its text, for the search of the site. */
export async function loadSearchIndex(): Promise<SearchEntry[]> {
  const nav = await loadNav()
  const entries: SearchEntry[] = []
  for (const item of nav.flatMap((s) => s.items)) {
    if (item.slug === undefined) continue
    const page = await render(item.slug)
    if (!page) continue
    for (const section of page.sections) {
      entries.push({ slug: item.slug, page: page.title, id: section.id, heading: section.heading, text: section.text })
    }
  }
  return entries
}

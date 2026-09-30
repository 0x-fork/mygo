import fs from "node:fs/promises"
import path from "node:path"
import type { Code, Root } from "mdast"
import remarkParse from "remark-parse"
import { unified } from "unified"

import type { HomeData } from "@/lib/home"
import { highlightCode } from "@/lib/markdown.server"

/** The example at the top of the docs: a Go service, and the page calling it. */
export async function loadHome(): Promise<HomeData> {
  const readme = await fs.readFile(path.resolve(process.cwd(), "../docs/README.md"), "utf8")
  const tree = unified().use(remarkParse).parse(readme) as Root
  const blocks = tree.children.filter((n): n is Code => n.type === "code")
  const go = blocks.find((b) => b.lang === "go")
  const ts = blocks.find((b) => b.lang === "ts")
  if (!go || !ts) throw new Error("docs/README.md: expected a go and a ts example")
  return { go: await highlightCode(go.value, "go"), ts: await highlightCode(ts.value, "ts") }
}

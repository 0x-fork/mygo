import { createServerFn } from "@tanstack/react-start"
import { staticFunctionMiddleware } from "@tanstack/start-static-server-functions"

import { loadHome } from "@/lib/home.server"

export interface HomeData {
  /** Highlighted code. */
  go: string
  ts: string
}

export const getHome = createServerFn({ method: "GET" })
  .middleware([staticFunctionMiddleware])
  .handler(() => loadHome())

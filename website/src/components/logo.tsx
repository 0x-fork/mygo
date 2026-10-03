import type * as React from "react"

import { cn } from "@/lib/utils"

// One "!" for each member of MyGO!!!!!, leaning this way and that.
const bangs = ["text-bang-1 -rotate-9", "text-bang-2 rotate-6", "text-bang-3 -rotate-4", "text-bang-4 rotate-8", "text-bang-5 -rotate-6"]

/** The name: MyGo, then the five "!" of MyGO!!!!!, in its members' colors. Read as "MyGo". */
export function Wordmark({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span className={cn("font-semibold tracking-tight", className)} {...props}>
      MyGo
      <span aria-hidden="true" className="ml-px font-extrabold tracking-normal">
        {bangs.map((bang) => (
          <span key={bang} className={cn("inline-block", bang)}>
            !
          </span>
        ))}
      </span>
    </span>
  )
}

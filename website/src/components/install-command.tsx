import { CheckIcon, CopyIcon } from "lucide-react"
import * as React from "react"

import { cn } from "@/lib/utils"

const command = "bunx mygo-cli init my-app"

export function InstallCommand({ className }: { className?: string }) {
  const [copied, setCopied] = React.useState(false)
  return (
    <button
      type="button"
      aria-label={`Copy "${command}"`}
      onClick={() =>
        void navigator.clipboard.writeText(command).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        })
      }
      className={cn(
        "group inline-flex h-11 items-center gap-3 rounded-lg border bg-card pr-3 pl-4 font-mono text-[13.5px] transition-colors hover:border-foreground/25",
        className
      )}
    >
      <span className="text-gopher-ink select-none">$</span>
      {command}
      <span className="ml-2 text-muted-foreground transition-colors group-hover:text-foreground">
        {copied ? <CheckIcon className="size-4" /> : <CopyIcon className="size-4" />}
      </span>
    </button>
  )
}

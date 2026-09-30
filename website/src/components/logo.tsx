import type * as React from "react"

/** An app icon with a ring, like the one `mygo init` gives new apps, in gopher blue. */
export function Logo(props: React.ComponentProps<"svg">) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" {...props}>
      <rect x="1" y="1" width="30" height="30" rx="7.5" className="fill-gopher" />
      <circle cx="16" cy="16" r="8.2" fill="none" stroke="#fff" strokeWidth="3.6" />
    </svg>
  )
}

import { Link, type LinkComponentProps } from "@tanstack/react-router"

/** A link to a page of the docs by its slug, "" for the introduction. */
export function DocLink({ slug, ...props }: { slug: string } & Omit<LinkComponentProps<"a">, "to" | "params">) {
  return slug ? (
    <Link to="/docs/$slug" params={{ slug }} {...props} />
  ) : (
    <Link to="/docs" activeOptions={{ exact: true }} {...props} />
  )
}

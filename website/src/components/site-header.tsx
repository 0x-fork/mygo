import { Link } from "@tanstack/react-router"

import { GitHubIcon } from "@/components/icons"
import { Logo } from "@/components/logo"
import { Search } from "@/components/search"
import { ThemeToggle } from "@/components/theme-toggle"
import { buttonVariants } from "@/components/ui/button"
import { site } from "@/lib/site"

const navLink = "text-muted-foreground transition-colors hover:text-foreground"

export function SiteHeader() {
  return (
    <header className="frame sticky top-0 z-40 flex h-14 items-center gap-6 border-b bg-background/80 px-4 backdrop-blur-xl sm:px-6">
      <Link to="/" className="flex items-center gap-2">
        <Logo className="size-5" />
        <span className="font-semibold tracking-tight">MyGo</span>
      </Link>
      <nav className="hidden items-center gap-5 text-sm sm:flex">
        <Link to="/docs" className={navLink} activeProps={{ className: "text-foreground!" }}>
          Docs
        </Link>
        <a href={`${site.repo}/tree/main/examples`} className={navLink}>
          Examples
        </a>
      </nav>
      <div className="ml-auto flex items-center gap-1">
        <Search />
        <a href={site.repo} aria-label="GitHub" className={buttonVariants({ variant: "ghost", size: "icon" })}>
          <GitHubIcon />
        </a>
        <ThemeToggle />
      </div>
    </header>
  )
}

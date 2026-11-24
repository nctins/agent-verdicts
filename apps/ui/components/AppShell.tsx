"use client";

import {
  ArrowLeftIcon, BookOpenIcon, ChevronRightIcon, FolderIcon, FolderKanbanIcon, HistoryIcon, KeyRoundIcon, LayoutDashboardIcon,
  LayoutGridIcon, MenuIcon, ScrollTextIcon, SearchIcon, ShieldIcon, UsersIcon, XIcon, type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState, type ReactNode } from "react";
import { Avatar } from "@/components/Avatar";
import { LocaleThemeSwitch } from "@/components/LocaleThemeSwitch";
import { Logo } from "@/components/Logo";
import { NetworkBanner } from "@/components/NetworkBanner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { can, canManage, managesAny, type Action } from "@/lib/access";
import { useLogout, useProjects, useSession } from "@/lib/api/queries";
import { parseRoute, projectHref } from "@/lib/routes";
import { CommandPalette } from "@/components/admin/CommandPalette";
import { ADMIN_GROUPS, ADMIN_LINKS, currentAdmin, HELP_GROUPS } from "@/lib/sections";
import { cn } from "@/lib/utils";

const item = "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent";
const active = "bg-accent font-medium";

/** Projects are the forum's categories: always in view, one click apart. */
/**
 * A parent menu with a collapse arrow. It starts open while you are inside it; the arrow
 * opens or folds it from anywhere, and that choice is kept while you move between pages.
 */
function Group({ id, head, label, defaultOpen, fold, setFold, children }: {
  id: string;
  head: ReactNode; // the parent row itself (a link, or a plain heading)
  label: string;
  defaultOpen: boolean;
  fold: Record<string, boolean>;
  setFold: (f: Record<string, boolean>) => void;
  children: ReactNode;
}) {
  const t = useTranslations("nav");
  const open = fold[id] ?? defaultOpen;
  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-center gap-1">
        <div className="min-w-0 flex-1">{head}</div>
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`menu-${id}`}
          aria-label={t("toggle", { label })}
          // Folding is not navigating: keep the mobile menu open.
          onClick={(e) => { e.stopPropagation(); setFold({ ...fold, [id]: !open }); }}
          className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <ChevronRightIcon className={cn("size-4 transition-transform", open && "rotate-90")} aria-hidden />
        </button>
      </div>
      {open && <div id={`menu-${id}`}>{children}</div>}
    </div>
  );
}

const ADMIN_ICONS: Record<string, LucideIcon> = {
  dashboard: LayoutDashboardIcon, actors: UsersIcon, roles: KeyRoundIcon, policies: ScrollTextIcon,
  manage_projects: FolderKanbanIcon, search_settings: SearchIcon, audit: HistoryIcon,
};

/** Inside /manage the sidebar is the admin console's own menu, grouped (spec 2026-11-22-admin-ui-redesign U1). */
function AdminNav({ links, at }: { links: (typeof ADMIN_LINKS)[number][]; at: string | null }) {
  const t = useTranslations("nav");
  return (
    <>
      <Link href="/" className={cn(item, "text-muted-foreground")}>
        <ArrowLeftIcon className="size-4" aria-hidden />
        {t("back_to_app")}
      </Link>
      <p className="flex items-center gap-2 px-2 text-sm font-semibold">
        <ShieldIcon className="size-4" aria-hidden />
        {t("admin_console")}
      </p>
      {ADMIN_GROUPS.map((g) => {
        const in_ = links.filter((l) => l[4] === g);
        if (in_.length === 0) return null;
        return (
          <div key={g || "top"} className="flex flex-col gap-0.5">
            {g && <p className="px-2 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{t(g)}</p>}
            {in_.map(([href, key]) => {
              const Icon = ADMIN_ICONS[key] ?? ShieldIcon;
              return (
                <Link key={href} href={href} aria-current={at === href ? "page" : undefined} className={cn(item, at === href && active)}>
                  <Icon className="size-4 text-muted-foreground" aria-hidden />
                  {t(key)}
                </Link>
              );
            })}
          </div>
        );
      })}
    </>
  );
}

/** Inside /help the sidebar is the guide's own menu, grouped (spec 2026-11-22-help-console H1). */
function HelpNav({ path }: { path: string }) {
  const t = useTranslations("nav");
  const th = useTranslations("help");
  return (
    <>
      <Link href="/" className={cn(item, "text-muted-foreground")}>
        <ArrowLeftIcon className="size-4" aria-hidden />
        {t("back_to_app")}
      </Link>
      <Link href="/help/" aria-current={path === "/help/" || path === "/help" ? "page" : undefined} className={cn(item, "font-semibold")}>
        <BookOpenIcon className="size-4" aria-hidden />
        {t("help")}
      </Link>
      {HELP_GROUPS.map(([g, sections]) => (
        <div key={g} className="flex flex-col gap-0.5">
          <p className="px-2 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{th(g)}</p>
          {sections.map((k) => {
            const here = path.startsWith(`/help/${k}`);
            return (
              <Link key={k} href={`/help/${k}/`} aria-current={here ? "page" : undefined} className={cn(item, "pl-3", here && active)}>
                {th(`pages.${k}.menu`)}
              </Link>
            );
          })}
        </div>
      ))}
    </>
  );
}

// Each page mounts its own AppShell, so the folds live here to survive client navigation
// (a reload starts fresh, matching the prerendered HTML).
let remembered: Record<string, boolean> = {};

function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  const t = useTranslations("nav");
  const path = usePathname();
  const current = parseRoute(path)?.project;
  const { data } = useProjects();
  const session = useSession().data;
  const manage = canManage(session);
  const links = ADMIN_LINKS.filter(([, , , need]) =>
    need === "manage" ? true : need === "people" ? can(session, "actor:Manage") || managesAny(session) : can(session, need as Action));
  const inHelp = path.startsWith("/help");
  const inAdmin = path.startsWith("/manage");
  const adminAt = currentAdmin(path);
  const [fold, setFoldState] = useState(remembered);
  const setFold = (f: Record<string, boolean>) => setFoldState((remembered = f));
  const group = { fold, setFold };
  if (inHelp) {
    return (
      <nav aria-label={t("menu")} className="flex flex-col gap-4 p-3" onClick={onNavigate}>
        <HelpNav path={path} />
      </nav>
    );
  }
  if (inAdmin && manage) {
    return (
      <nav aria-label={t("menu")} className="flex flex-col gap-4 p-3" onClick={onNavigate}>
        <AdminNav links={links} at={adminAt} />
      </nav>
    );
  }
  return (
    <nav aria-label={t("menu")} className="flex flex-col gap-4 p-3" onClick={onNavigate}>
      <Link href="/" className={cn(item, path === "/" && active)}>
        <LayoutGridIcon className="size-4" aria-hidden />
        {t("projects")}
      </Link>
      <Group
        {...group}
        id="projects"
        label={t("section_projects")}
        defaultOpen
        head={<p className="px-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("section_projects")}</p>}
      >
        {data?.items.filter((p) => !p.archived || p.id === current).map((p) => (
          <Link
            key={p.id}
            href={projectHref(p.id)}
            aria-current={p.id === current ? "page" : undefined}
            className={cn(item, "justify-between", p.id === current && active)}
          >
            <span className="flex min-w-0 items-center gap-2" title={p.name !== p.id ? p.id : undefined}>
              <FolderIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
              <span className="truncate">{p.name}</span>
            </span>
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground tabular-nums">
              {/* The conflict count is on the project page; here a dot only says "something to settle". */}
              {p.states.Conflict > 0 && <span aria-hidden title={t("has_conflicts")} className="size-2 rounded-full bg-conflict" />}
              {p.contribution_count}
            </span>
          </Link>
        ))}
      </Group>
      <div className="flex flex-col gap-0.5">
        <p className="px-2 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("section_more")}</p>
        {/* One link, like Admin: the guide has its own menu once inside. */}
        <Link href="/help/" className={cn(item, inHelp && active)}>
          <BookOpenIcon className="size-4" aria-hidden />
          {t("help")}
        </Link>
        {/* One link: the admin console has its own menu once inside. */}
        {manage && (
          <Link href="/manage/" className={cn(item, inAdmin && active)}>
            <ShieldIcon className="size-4" aria-hidden />
            {t("admin")}
          </Link>
        )}
      </div>
    </nav>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const t = useTranslations("nav");
  const tk = useTranslations("kit.actor_type");
  const router = useRouter();
  const { data: s } = useSession();
  const logout = useLogout();
  // The menu belongs to the page it was opened on: any navigation (logo, back, in-page link) closes it.
  const path = usePathname();
  const [menuAt, setMenuAt] = useState<string | null>(null);
  const menu = menuAt === path;
  const setMenu = (open: boolean) => setMenuAt(open ? path : null);
  const signOut = () => logout.mutate(undefined, { onSuccess: () => router.replace("/login/") });

  return (
    <>
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-background focus:px-3 focus:py-2"
      >
        {t("skip_link")}
      </a>
      <header className="sticky top-0 z-30 border-b bg-background/95 backdrop-blur">
        <div className="flex h-14 items-center justify-between gap-2 px-3">
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label={t("open_menu")}
              aria-expanded={menu}
              onClick={() => setMenu(!menu)}
            >
              {menu ? <XIcon /> : <MenuIcon />}
            </Button>
            <Link href="/" className="flex items-center gap-2 px-2 font-semibold tracking-tight">
              <Logo />
              <span className="max-sm:sr-only">Agent Verdicts</span>
            </Link>
          </div>
          <div className="flex items-center gap-1">
            <LocaleThemeSwitch />
            {/* After the always-there controls: inserting a client-only element before them would shift their ids and break hydration. */}
            {s?.authenticated && <CommandPalette />}
            {s?.authenticated && s.actor ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" className="gap-2 px-1.5" aria-label={t("account_menu", { name: s.actor.name })}>
                    <Avatar name={s.actor.name} type={s.actor.type} seed={s.actor.id} className="size-7" />
                    <span className="max-w-32 truncate max-sm:hidden">{s.actor.name}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-48">
                  <DropdownMenuLabel className="flex flex-col">
                    <span>{s.actor.name}</span>
                    <span className="text-xs font-normal text-muted-foreground">{tk(s.actor.type)}</span>
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem asChild><Link href="/account/">{t("account")}</Link></DropdownMenuItem>
                  <DropdownMenuItem onSelect={signOut}>{t("logout")}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : (
              s && <Button asChild><Link href="/login/">{t("login")}</Link></Button>
            )}
          </div>
        </div>
        {menu && (
          <div className="border-t bg-sidebar md:hidden">
            <Sidebar onNavigate={() => setMenu(false)} />
          </div>
        )}
      </header>
      <NetworkBanner />
      <div className="flex flex-1">
        <aside className="sticky top-14 hidden h-[calc(100vh-3.5rem)] w-60 shrink-0 overflow-y-auto border-r bg-sidebar md:block">
          <Sidebar />
        </aside>
        <main id="main" className="mx-auto w-full max-w-6xl min-w-0 flex-1 p-4 md:p-6">
          {children}
        </main>
      </div>
    </>
  );
}

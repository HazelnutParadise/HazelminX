import type { Icon } from "@phosphor-icons/react";
import {
  ArchiveIcon,
  BellIcon,
  BrainIcon,
  ChartLineIcon,
  ClockIcon,
  CubeIcon,
  DatabaseIcon,
  GearSixIcon,
  GlobeIcon,
  HardDrivesIcon,
  ListChecksIcon,
  PlugsConnectedIcon,
  ScrollIcon,
  ShieldCheckIcon,
  SquaresFourIcon,
  StackIcon,
  SteeringWheelIcon,
  StorefrontIcon,
  TerminalWindowIcon,
  UsersThreeIcon,
} from "@phosphor-icons/react";

/**
 * Every top-level page of the shell. One entry per page; the sidebar, the mobile chip bar,
 * the router and the page titles all read this list (DESIGN.md「資訊架構」).
 * `key` doubles as the i18n key under `nav.*`.
 */
export interface PageDef {
  key: PageKey;
  path: string;
  icon: Icon;
  /** Shows a count badge in the sidebar when the shell has a value for it. */
  counter?: "jobs" | "attention";
}

export type PageKey =
  | "overview"
  | "jobs"
  | "events"
  | "projects"
  | "appStore"
  | "websites"
  | "databases"
  | "ai"
  | "apiProxy"
  | "nodes"
  | "containers"
  | "kubernetes"
  | "storage"
  | "cron"
  | "security"
  | "analytics"
  | "logs"
  | "iam"
  | "settings"
  | "terminal";

export const PAGES: Record<PageKey, PageDef> = {
  overview: { key: "overview", path: "/", icon: SquaresFourIcon },
  jobs: { key: "jobs", path: "/jobs", icon: ListChecksIcon, counter: "jobs" },
  events: { key: "events", path: "/events", icon: BellIcon, counter: "attention" },
  projects: { key: "projects", path: "/projects", icon: StackIcon },
  appStore: { key: "appStore", path: "/app-store", icon: StorefrontIcon },
  websites: { key: "websites", path: "/websites", icon: GlobeIcon },
  databases: { key: "databases", path: "/databases", icon: DatabaseIcon },
  ai: { key: "ai", path: "/ai", icon: BrainIcon },
  apiProxy: { key: "apiProxy", path: "/api-proxy", icon: PlugsConnectedIcon },
  nodes: { key: "nodes", path: "/nodes", icon: HardDrivesIcon },
  containers: { key: "containers", path: "/containers", icon: CubeIcon },
  kubernetes: { key: "kubernetes", path: "/kubernetes", icon: SteeringWheelIcon },
  storage: { key: "storage", path: "/storage", icon: ArchiveIcon },
  cron: { key: "cron", path: "/cron", icon: ClockIcon },
  security: { key: "security", path: "/security", icon: ShieldCheckIcon },
  analytics: { key: "analytics", path: "/analytics", icon: ChartLineIcon },
  logs: { key: "logs", path: "/logs", icon: ScrollIcon },
  iam: { key: "iam", path: "/iam", icon: UsersThreeIcon },
  settings: { key: "settings", path: "/settings", icon: GearSixIcon },
  terminal: { key: "terminal", path: "/terminal", icon: TerminalWindowIcon },
};

/** Sidebar order: three ungrouped items first, then the four groups (`navGroup.*` i18n keys). */
export const NAV_TOP: PageKey[] = ["overview", "jobs", "events"];

export const NAV_GROUPS: { key: "apps" | "infra" | "securityLogs" | "admin"; pages: PageKey[] }[] = [
  { key: "apps", pages: ["projects", "appStore", "websites", "databases", "ai", "apiProxy"] },
  { key: "infra", pages: ["nodes", "containers", "kubernetes", "storage", "cron"] },
  { key: "securityLogs", pages: ["security", "analytics", "logs"] },
  { key: "admin", pages: ["iam", "settings"] },
];

/** All pages reachable from the sidebar, in display order (terminal lives in the top bar). */
export const NAV_ORDER: PageKey[] = [...NAV_TOP, ...NAV_GROUPS.flatMap((g) => g.pages)];

/** The sidebar entry that owns a pathname: exact match for "/", otherwise the longest path prefix. */
export function pageForPath(pathname: string): PageKey | undefined {
  if (pathname === "/") return "overview";
  let best: PageDef | undefined;
  for (const p of Object.values(PAGES)) {
    if (p.path === "/") continue;
    if ((pathname === p.path || pathname.startsWith(p.path + "/")) && (!best || p.path.length > best.path.length)) {
      best = p;
    }
  }
  return best?.key;
}

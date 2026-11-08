import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { useState } from "react";
import { Outlet } from "react-router";
import { useTranslation } from "react-i18next";
import { PAGES } from "../app/navigation";
import { useShellStatus } from "../app/shellStatus";
import { HostBar } from "../components/HostBar";
import { PREF_KEYS, readPref, writePref } from "../prefs/storage";
import { useMediaQuery } from "./useMediaQuery";
import { MobileNav } from "./MobileNav";
import { SideNav } from "./SideNav";
import { TopBar } from "./TopBar";
import "./shell.css";

/** Below this width the sidebar becomes a horizontal chip row (DESIGN.md Layout). */
export const MOBILE_QUERY = "(max-width: 767px)";

export function AppShell() {
  const { t } = useTranslation();
  const status = useShellStatus();
  const mobile = useMediaQuery(MOBILE_QUERY);
  const [sidebarOpen, setSidebarOpen] = useState(() => readPref(PREF_KEYS.sidebar) !== "collapsed");
  const onSidebarChange = (open: boolean) => {
    setSidebarOpen(open);
    writePref(PREF_KEYS.sidebar, open ? "open" : "collapsed");
  };

  return (
    <div className="hx-shell">
      <a className="hx-skip" href="#main">
        {t("shell.skipToContent")}
      </a>
      <TopBar />
      {status.unreachableHosts && status.unreachableHosts.length > 0 && (
        <HostBar
          hosts={status.unreachableHosts}
          allHref={`${PAGES.nodes.path}?status=unreachable`}
          hostHref={(id) => `${PAGES.nodes.path}/${encodeURIComponent(id)}`}
        />
      )}
      {/* One tree for every width: <main> keeps its position, so crossing the phone breakpoint
          swaps only the navigation and never remounts the page (no lost input). */}
      <Sidebar.Provider
        className={mobile ? "hx-body hx-body--mobile" : "hx-body"}
        open={sidebarOpen}
        collapsible="icon"
        mobileBreakpoint={0}
        onOpenChange={onSidebarChange}
      >
        {mobile ? <MobileNav /> : <SideNav />}
        <main id="main" className="hx-main" tabIndex={-1}>
          <Outlet />
        </main>
      </Sidebar.Provider>
    </div>
  );
}

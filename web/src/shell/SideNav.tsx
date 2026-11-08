import { Sidebar, useSidebar } from "@cloudflare/kumo/components/sidebar";
import { SidebarSimpleIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { useLocation } from "react-router";
import { NAV_GROUPS, NAV_TOP, PAGES, pageForPath, type PageKey } from "../app/navigation";
import { NavCount } from "./NavCount";

function Item({ page, current }: { page: PageKey; current?: PageKey }) {
  const { t } = useTranslation();
  const def = PAGES[page];
  const label = t(`nav.${page}`);
  return (
    <Sidebar.MenuItem>
      <Sidebar.MenuButton
        href={def.path}
        icon={def.icon}
        active={page === current}
        aria-current={page === current ? "page" : undefined}
        tooltip={label}
      >
        <span className="hx-nav-label">{label}</span>
        {def.counter && <NavCount kind={def.counter} />}
      </Sidebar.MenuButton>
    </Sidebar.MenuItem>
  );
}

function CollapseButton() {
  const { t } = useTranslation();
  const { open, toggleSidebar } = useSidebar();
  const label = open ? t("shell.collapseNav") : t("shell.expandNav");
  return (
    <Sidebar.MenuButton icon={SidebarSimpleIcon} onClick={toggleSidebar} aria-label={label} tooltip={label}>
      {t("shell.collapse")}
    </Sidebar.MenuButton>
  );
}

export function SideNav() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const current = pageForPath(pathname);
  return (
    <Sidebar className="hx-side" role="navigation" aria-label={t("shell.mainNav")}>
      <Sidebar.Content className="hx-scroll">
        <Sidebar.Group>
          <Sidebar.Menu>
            {NAV_TOP.map((p) => (
              <Item key={p} page={p} current={current} />
            ))}
          </Sidebar.Menu>
        </Sidebar.Group>
        {NAV_GROUPS.map((g) => (
          <Sidebar.Group key={g.key}>
            <Sidebar.GroupLabel>{t(`navGroup.${g.key}`)}</Sidebar.GroupLabel>
            <Sidebar.Menu>
              {g.pages.map((p) => (
                <Item key={p} page={p} current={current} />
              ))}
            </Sidebar.Menu>
          </Sidebar.Group>
        ))}
      </Sidebar.Content>
      <Sidebar.Footer>
        <Sidebar.Menu>
          <Sidebar.MenuItem>
            <CollapseButton />
          </Sidebar.MenuItem>
        </Sidebar.Menu>
      </Sidebar.Footer>
    </Sidebar>
  );
}

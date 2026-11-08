import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { NavLink, useLocation } from "react-router";
import { NAV_ORDER, PAGES, pageForPath } from "../app/navigation";
import { NavCount } from "./NavCount";

/**
 * Phone navigation: one horizontally scrolling row of chips. The current page is scrolled into view
 * on every navigation instead of staying at the left edge (DESIGN.md「手機導覽列自動捲到目前頁」).
 */
export function MobileNav() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const current = pageForPath(pathname);
  const rowRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const row = rowRef.current;
    const chip = row?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!row || !chip) return;
    const left = chip.offsetLeft - (row.clientWidth - chip.offsetWidth) / 2;
    row.scrollTo({ left: Math.max(0, left), behavior: "instant" as ScrollBehavior });
  }, [current]);

  return (
    <nav className="hx-chipnav" aria-label={t("shell.mainNav")}>
      <div ref={rowRef} className="hx-chiprow hx-scroll">
        {NAV_ORDER.map((key) => (
          <NavLink
            key={key}
            to={PAGES[key].path}
            className="hx-chip"
            aria-current={key === current ? "page" : undefined}
            end={PAGES[key].path === "/"}
          >
            {t(`nav.${key}`)}
            {PAGES[key].counter && <NavCount kind={PAGES[key].counter} />}
          </NavLink>
        ))}
      </div>
    </nav>
  );
}

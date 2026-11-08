import { LinkButton } from "@cloudflare/kumo/components/button";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import "./system.css";

/**
 * Site-wide host status row under the top bar (DESIGN.md 主機狀態列). SLOT — props are fixed.
 * Not dismissible; it disappears when `hosts` becomes empty. Maintenance hosts are excluded by the caller.
 */
export interface HostBarProps {
  hosts: { id: string; name: string }[];
  /** Link to the host list filtered to unreachable hosts. */
  allHref: string;
  /** Link to one host. */
  hostHref: (id: string) => string;
}

export function HostBar({ hosts, allHref, hostHref }: HostBarProps) {
  const { t } = useTranslation();

  if (hosts.length === 0) {
    return null;
  }

  const visibleHosts = hosts.slice(0, 3);
  const remainingCount = hosts.length - 3;

  return (
    <div className="hx-hostbar" role="status" aria-label={t("hostBar.label")}>
      <span className="hx-status-pill hx-status-pill--danger">
        <span className="hx-status-shape" aria-hidden="true">
          ■
        </span>
        <span>{t("hostBar.unreachable", { count: hosts.length })}</span>
      </span>

      {visibleHosts.map((host) => (
        <Link key={host.id} to={hostHref(host.id)} className="hx-hostbar-host">
          {host.name}
        </Link>
      ))}

      {remainingCount > 0 && (
        <span className="hx-hostbar-others">{t("hostBar.others", { count: remainingCount })}</span>
      )}

      <LinkButton href={allHref} size="sm" variant="secondary" className="hx-hostbar-all">
        {t("hostBar.viewAll")}
      </LinkButton>
    </div>
  );
}

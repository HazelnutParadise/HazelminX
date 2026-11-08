import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { StatusPill } from "./StatusPill";
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
      <StatusPill tone="danger">{t("hostBar.unreachable", { count: hosts.length })}</StatusPill>

      {visibleHosts.map((host) => (
        <Link key={host.id} to={hostHref(host.id)} className="hx-hostbar-host">
          {host.name}
        </Link>
      ))}

      {remainingCount > 0 && (
        <span className="hx-hostbar-others">{t("hostBar.others", { count: remainingCount })}</span>
      )}

      <LinkButton href={hostHref(hosts[0]!.id)} size="sm" variant="secondary" className="hx-hostbar-handle">
        {t("hostBar.handle")}
      </LinkButton>
      <LinkButton href={allHref} size="sm" variant="ghost" className="hx-hostbar-all">
        {t("hostBar.viewAll")}
        <ArrowRightIcon size={14} aria-hidden="true" />
      </LinkButton>
    </div>
  );
}

import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { LockIcon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { isApiError } from "../auth/api";
import { Notice } from "../auth/parts";
import { ApiErrorMessage } from "../components/RawMessage";
import { StatusPill } from "../components/StatusPill";
import type { ApiError } from "../api/client";
import { activeLocale } from "../i18n";
import { formatRelative } from "../lib/time";
import { useNow } from "../shell/useNow";
import type { Node } from "./api";
import { at, certificateStatus, hostStatus, pendingSync } from "./status";

/** Host status pill (connection or trust state); offline and stale also say the channel is what failed. */
export function HostStatusPill({ node }: { node: Node }) {
  const { t } = useTranslation();
  const s = hostStatus(node);
  return <StatusPill tone={s.tone}>{t(`nodes.status.${s.key}`)}</StatusPill>;
}

export function CertificatePill({ node }: { node: Node }) {
  const { t } = useTranslation();
  const s = certificateStatus(node);
  return <StatusPill tone={s.tone}>{t(`nodes.cert.${s.key}`, { days: node.certificate.expiresInDays })}</StatusPill>;
}

/** What has not reached the host yet; "—" when nothing is waiting. */
export function PendingSync({ node }: { node: Node }) {
  const { t } = useTranslation();
  const list = pendingSync(node);
  if (list.length === 0) {
    return (
      <span>
        <span aria-hidden="true">—</span>
        <span className="hx-visually-hidden">{t("nodes.sync.none")}</span>
      </span>
    );
  }
  return (
    <ul className="hx-sync-list">
      {list.map((s) => (
        <li key={s}>
          <StatusPill tone="warn">{t(`nodes.sync.${s}`, { defaultValue: s })}</StatusPill>
        </li>
      ))}
    </ul>
  );
}

/** "48 秒前" with the full time in the tooltip; never contacted reads as such, not as a time. */
export function LastContact({ node }: { node: Node }) {
  const { t } = useTranslation();
  const now = useNow(5_000);
  const iso = node.connection.lastContactAt ?? undefined;
  const rel = formatRelative(iso, now, activeLocale(), t("nodes.justNow"));
  if (!rel) return <span className="hx-muted">{t("nodes.noContact")}</span>;
  return (
    <time dateTime={iso} title={at(iso)}>
      {rel}
    </time>
  );
}

/** A label and a value, used in the host detail and the enrollment check (board AddNode「核對主機」). */
export function Field({ label, children, mono = false }: { label: string; children: ReactNode; mono?: boolean }) {
  return (
    <div className="hx-field">
      <dt>{label}</dt>
      <dd className={mono ? "hx-mono" : undefined}>{children}</dd>
    </div>
  );
}

/**
 * A write button the user may not be allowed to press: shown disabled with a lock and the permission it needs
 * (DESIGN.md「權限不足的動作不藏」). `allowed` undefined (still loading) also disables it, without the lock.
 */
export function WriteButton({
  allowed,
  children,
  variant = "secondary",
  type = "button",
  disabled = false,
  onClick,
}: {
  allowed: boolean | undefined;
  children: ReactNode;
  variant?: "primary" | "secondary" | "secondary-destructive" | "destructive";
  type?: "button" | "submit";
  disabled?: boolean;
  onClick?: () => void;
}) {
  const { t } = useTranslation();
  if (allowed === false) {
    return (
      <Tooltip content={t("nodes.needWrite")} render={<span tabIndex={0} className="hx-lock-wrap" />}>
        <Button variant={variant} type={type} disabled icon={<LockIcon aria-hidden="true" />}>
          {children}
          <span className="hx-visually-hidden">{t("nodes.needWrite")}</span>
        </Button>
      </Tooltip>
    );
  }
  return (
    <Button variant={variant} type={type} onClick={onClick} disabled={disabled || allowed === undefined}>
      {children}
    </Button>
  );
}

/** Message codes of host and enrollment calls with a sentence of their own. */
const MESSAGES: Record<string, string> = {
  VERSION_MISMATCH: "nodes.errors.versionMismatch",
  NOT_AVAILABLE: "nodes.errors.notAvailable",
  FORBIDDEN: "nodes.errors.forbidden",
  PLATFORM_DEGRADED: "nodes.errors.degraded",
};

/** A failed host or enrollment call: our sentence for known codes, Core's text kept underneath. */
export function NodeError({ error }: { error: unknown }) {
  const { t } = useTranslation();
  if (!error) return null;
  if (isApiError(error)) {
    const e = error as ApiError;
    const key = MESSAGES[e.code];
    return (
      <div role="alert" className="hx-node-error">
        {key && <Notice tone="danger">{t(key)}</Notice>}
        <ApiErrorMessage error={e} />
      </div>
    );
  }
  return <Notice tone="danger">{error instanceof Error ? error.message : JSON.stringify(error)}</Notice>;
}

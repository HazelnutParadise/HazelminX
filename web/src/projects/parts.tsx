import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { LockIcon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ApiError } from "../api/client";
import { isApiError } from "../auth/api";
import { Notice } from "../auth/parts";
import { ApiErrorMessage } from "../components/RawMessage";
import { StatusPill, type StatusTone } from "../components/StatusPill";
import { at } from "../nodes/status";

const SEVERITY_TONE: Record<string, StatusTone> = {
  error: "danger",
  "high-risk": "danger",
  warning: "warn",
  kept: "info",
};
const DEPLOY_TONE: Record<string, StatusTone> = { running: "info", succeeded: "ok", failed: "danger" };
const PLAN_TONE: Record<string, StatusTone> = {
  awaiting_approval: "warn",
  approved: "ok",
  executed: "ok",
  rejected: "danger",
  withdrawn: "info",
  expired: "info",
  invalidated: "danger",
};
const CHECK_TONE: Record<string, StatusTone> = { pass: "ok", warning: "warn", high_risk: "danger", failed: "danger" };
const JOB_TONE: Record<string, StatusTone> = {
  succeeded: "ok",
  failed: "danger",
  cancelled: "info",
  needs_attention: "danger",
};
const STEP_TONE: Record<string, StatusTone> = { done: "ok", failed: "danger", running: "info", pending: "info" };

function Pill({
  tones,
  group,
  value,
}: {
  tones: Record<string, StatusTone>;
  group: string;
  value: string | null | undefined;
}) {
  const { t } = useTranslation();
  const v = value ?? "";
  return <StatusPill tone={tones[v] ?? "info"}>{t(`projects.${group}.${v}`, { defaultValue: v || "—" })}</StatusPill>;
}

export const SeverityPill = ({ value }: { value: string | null | undefined }) => (
  <Pill tones={SEVERITY_TONE} group="severity" value={value} />
);
export const DeploymentPill = ({ value }: { value: string | null | undefined }) => (
  <Pill tones={DEPLOY_TONE} group="deployState" value={value} />
);
export const PlanPill = ({ value }: { value: string | null | undefined }) => (
  <Pill tones={PLAN_TONE} group="planState" value={value} />
);
export const CheckPill = ({ value }: { value: string | null | undefined }) => (
  <Pill tones={CHECK_TONE} group="checkState" value={value} />
);
export const JobPill = ({ value }: { value: string | null | undefined }) => (
  <Pill tones={JOB_TONE} group="jobState" value={value} />
);
export const StepPill = ({ value }: { value: string | null | undefined }) => (
  <Pill tones={STEP_TONE} group="stepState" value={value} />
);

/** Absolute local time, or "—". */
export function When({ iso }: { iso: string | null | undefined }) {
  if (!iso) return <span aria-hidden="true">—</span>;
  return <time dateTime={iso}>{at(iso)}</time>;
}

/** A section of the environment's content area: heading row, then its body. */
export function Panel({
  id,
  title,
  extra,
  children,
}: {
  id: string;
  title: ReactNode;
  extra?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="hx-panel" aria-labelledby={id}>
      <div className="hx-panel-head">
        <h2 id={id}>{title}</h2>
        {extra && <div className="hx-panel-extra">{extra}</div>}
      </div>
      {children}
    </section>
  );
}

/**
 * A button the user may not be allowed to press: disabled with a lock and the permission it needs
 * (DESIGN.md「權限不足的動作不藏」). `allowed` undefined (still loading) also disables it.
 */
export function GuardedButton({
  allowed,
  need,
  children,
  variant = "secondary",
  disabled = false,
  loading = false,
  onClick,
  type = "button",
}: {
  allowed: boolean | undefined;
  /** What is missing, shown in the tooltip ("需要 projects:deploy 權限"). */
  need: string;
  children: ReactNode;
  variant?: "primary" | "secondary" | "secondary-destructive" | "destructive";
  disabled?: boolean;
  loading?: boolean;
  onClick?: () => void;
  type?: "button" | "submit";
}) {
  if (allowed === false) {
    return (
      <Tooltip content={need} render={<span tabIndex={0} className="hx-lock-wrap" />}>
        <Button variant={variant} type={type} disabled icon={<LockIcon aria-hidden="true" />}>
          {children}
          <span className="hx-visually-hidden">{need}</span>
        </Button>
      </Tooltip>
    );
  }
  return (
    <Button
      variant={variant}
      type={type}
      onClick={onClick}
      loading={loading}
      disabled={disabled || allowed === undefined}
    >
      {children}
    </Button>
  );
}

const MESSAGES: Record<string, string> = {
  VERSION_MISMATCH: "projects.errors.versionMismatch",
  ALREADY_EXISTS: "projects.errors.alreadyExists",
  FORBIDDEN: "projects.errors.forbidden",
  CHANGE_PLAN_CHANGED: "projects.errors.planChanged",
  VALIDATION_FAILED: "projects.errors.validation",
};

/** A failed call: our sentence for known codes, Core's text kept underneath. */
export function ProjectError({ error }: { error: unknown }) {
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

/**
 * Text Core identifies by a code: the interface-language sentence for the first key that exists, with Core's
 * original text underneath as secondary detail (DESIGN.md「原文加白話摘要」). With no translation for any key,
 * Core's text is shown as it is, so a new code is never hidden.
 */
export function Coded({
  keys,
  raw,
  values,
}: {
  keys: string[];
  raw?: string | null;
  values?: Record<string, unknown>;
}) {
  const { t, i18n } = useTranslation();
  const key = keys.find((k) => i18n.exists(k));
  if (!key) return <span className="hx-raw">{raw || "—"}</span>;
  return (
    <span className="hx-coded">
      <span>{t(key as never, values as never) as unknown as string}</span>
      {raw && (
        <span className="hx-original" lang="en">
          <span className="hx-visually-hidden">{t("projects.code.original")}：</span>
          {raw}
        </span>
      )}
    </span>
  );
}

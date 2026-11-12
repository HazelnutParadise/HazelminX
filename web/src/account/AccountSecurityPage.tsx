import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Select } from "@cloudflare/kumo/components/select";
import { Table } from "@cloudflare/kumo/components/table";
import { PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import {
  authApi,
  authKeys,
  RECOVERY_CODES_PER_SET,
  type EnrollmentResult,
  type FactorSummary,
  type SessionInfo,
  type SessionSettings,
} from "../auth/api";
import { PasskeyForm, TotpSetup } from "../auth/FactorSetup";
import { AuthError, RecoveryCodes } from "../auth/parts";
import { ReauthCancelled, useReauth } from "../auth/reauth";
import { useSession } from "../auth/session";
import { describeUserAgent } from "../auth/webauthn";
import { StatusPill, type StatusTone } from "../components/StatusPill";
import { activeLocale } from "../i18n";
import { browserTimeZone, formatDateTime, formatRelative } from "../lib/time";
import { PageHead } from "../shell/PageShell";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { useNow } from "../shell/useNow";
import "./account.css";

function shortDate(iso: string): string {
  return formatDateTime(iso, activeLocale(), browserTimeZone(), "date") ?? iso;
}

/** One row of the second-factor table: the authenticator app or one passkey. */
interface FactorRow {
  key: string;
  kind: "totp" | "passkey";
  id?: string;
  name: string;
  detail: string;
  lastUsedAt?: string;
}

function factorRows(f: FactorSummary, t: TFunction): FactorRow[] {
  const rows: FactorRow[] = f.passkeys.map((p) => ({
    key: `passkey:${p.id}`,
    kind: "passkey",
    id: p.id,
    name: p.name,
    detail: t("account.factors.passkeyDetail", { rpId: p.rpId, date: shortDate(p.createdAt) }),
    lastUsedAt: p.lastUsedAt,
  }));
  const totp = f.totp;
  if (totp?.confirmed) {
    rows.push({
      key: "totp",
      kind: "totp",
      name: t("account.factors.app"),
      detail: t("account.factors.added", { date: shortDate(totp.confirmedAt ?? totp.createdAt) }),
      lastUsedAt: totp.lastUsedAt,
    });
  }
  return rows;
}

/**
 * The signed-in user's own security (board AccountSecurity, ticket 014 API): second factors, recovery codes,
 * signed-in devices and the idle timeout.
 */
export function AccountSecurityPage() {
  const { t } = useTranslation();
  const session = useSession();
  const username = session.status === "signed-in" ? session.session.user.username : "";
  const title = username ? t("account.title", { username }) : t("nav.accountSecurity");
  useDocumentTitle(t("nav.accountSecurity"));
  return (
    <>
      <PageHead title={title} />
      <div className="hx-account">
        <FactorsPanel />
        <RecoveryPanel />
        <SessionsPanel />
        <IdlePanel />
      </div>
    </>
  );
}

function Panel({
  title,
  actions,
  children,
  id,
}: {
  title: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  id: string;
}) {
  return (
    <section className="hx-panel" aria-labelledby={id}>
      <div className="hx-panel-head">
        <h2 id={id}>{title}</h2>
        {actions && <div className="hx-panel-actions">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

function PanelBody({ children }: { children: React.ReactNode }) {
  return <div className="hx-panel-body">{children}</div>;
}

/** Shown when a reauthentication was cancelled: nothing changed, no error to report. */
function reported(err: unknown): unknown {
  return err instanceof ReauthCancelled ? null : err;
}

function FactorsPanel() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const factors = useQuery({ queryKey: authKeys.factors, queryFn: authApi.factors });
  const now = useNow(60_000);
  const [adding, setAdding] = useState<"passkey" | "totp" | null>(null);
  const [removing, setRemoving] = useState<FactorRow | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const rows = factors.data ? factorRows(factors.data, t) : [];

  const added = (r: EnrollmentResult) => {
    setAdding(null);
    void client.invalidateQueries({ queryKey: authKeys.factors });
    void client.invalidateQueries({ queryKey: authKeys.sessions });
    if (r.recoveryCodes?.length) setCodes(r.recoveryCodes);
  };

  return (
    <Panel
      id="hx-factors"
      title={factors.data ? t("account.factors.titleCount", { count: rows.length }) : t("account.factors.title")}
    >
      {factors.isError ? (
        <PanelBody>
          <AuthError error={factors.error} />
        </PanelBody>
      ) : (
        <div className="hx-table-wrap">
          <Table className="hx-table">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("account.factors.name")}</Table.Head>
                <Table.Head>{t("account.factors.type")}</Table.Head>
                <Table.Head>{t("account.factors.lastUsed")}</Table.Head>
                <Table.Head>
                  <span className="hx-visually-hidden">{t("account.actions")}</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.length === 0 && factors.data && (
                <Table.Row>
                  <Table.Cell colSpan={4} className="hx-muted-cell">
                    {t("account.factors.none")}
                  </Table.Cell>
                </Table.Row>
              )}
              {rows.map((r) => (
                <Table.Row key={r.key}>
                  <Table.Cell>
                    <div className="hx-name">{r.name}</div>
                    <div className="hx-sub">{r.detail}</div>
                  </Table.Cell>
                  <Table.Cell>
                    <StatusPill tone="info">
                      {r.kind === "passkey" ? "Passkey" : t("account.factors.typeApp")}
                    </StatusPill>
                  </Table.Cell>
                  <Table.Cell className="hx-nowrap">
                    {formatRelative(r.lastUsedAt, now, activeLocale(), t("account.justNow")) ??
                      t("account.factors.neverUsed")}
                  </Table.Cell>
                  <Table.Cell className="hx-acts">
                    <button
                      type="button"
                      className="hx-ib hx-ib--line"
                      aria-label={t("account.factors.removeNamed", { name: r.name })}
                      title={t("account.factors.remove")}
                      onClick={() => setRemoving(r)}
                    >
                      <TrashIcon size={16} aria-hidden="true" />
                    </button>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
      <div className="hx-panel-foot">
        <Button
          variant="secondary"

          icon={<PlusIcon aria-hidden="true" />}
          onClick={() => setAdding("passkey")}
        >
          {t("account.factors.addPasskey")}
        </Button>
        {factors.data && !factors.data.totp?.confirmed && (
          <Button
            variant="secondary"

            icon={<PlusIcon aria-hidden="true" />}
            onClick={() => setAdding("totp")}
          >
            {t("account.factors.addApp")}
          </Button>
        )}
      </div>

      {adding && (
        <Dialog.Root open disablePointerDismissal onOpenChange={(open: boolean) => !open && setAdding(null)}>
          <Dialog className="hx-dialog" size="base">
            <Dialog.Title className="hx-dialog-title">
              {adding === "passkey" ? t("account.factors.addPasskey") : t("account.factors.addApp")}
            </Dialog.Title>
            <div className="hx-dialog-body">
              {adding === "passkey" ? (
                <PasskeyForm onEnrolled={added} onCancel={() => setAdding(null)} />
              ) : (
                <TotpSetup
                  variant="dialog"
                  submitLabel={t("account.factors.bind")}
                  onEnrolled={added}
                  onCancel={() => setAdding(null)}
                />
              )}
            </div>
          </Dialog>
        </Dialog.Root>
      )}
      {removing && factors.data && (
        <RemoveFactorDialog
          row={removing}
          all={rows}
          required={factors.data.required}
          onClose={() => setRemoving(null)}
        />
      )}
      {codes && <CodesDialog codes={codes} onClose={() => setCodes(null)} />}
    </Panel>
  );
}

function RemoveFactorDialog({
  row,
  all,
  required,
  onClose,
}: {
  row: FactorRow;
  all: FactorRow[];
  required: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const reauth = useReauth();
  const others = all.filter((r) => r.key !== row.key);
  const last = others.length === 0;
  const remove = useMutation({
    mutationFn: () =>
      reauth(
        () => (row.kind === "passkey" ? authApi.removePasskey(row.id!) : authApi.removeTotp()),
        t("account.factors.removeAction", { name: row.name }),
      ),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: authKeys.factors });
      void client.invalidateQueries({ queryKey: authKeys.sessions });
      onClose();
    },
  });
  let check: { tone: StatusTone; title: string; detail: string };
  if (!last) {
    check = {
      tone: "ok",
      title: t("account.factors.stillCanSignIn"),
      detail: t("account.factors.remaining", { names: others.map((o) => o.name).join(t("account.listSeparator")) }),
    };
  } else if (required) {
    check = {
      tone: "danger",
      title: t("account.factors.lastRequired"),
      detail: t("account.factors.lastRequiredDetail"),
    };
  } else {
    check = { tone: "warn", title: t("account.factors.lastOptional"), detail: t("account.factors.lastOptionalDetail") };
  }
  const shape = { ok: "●", warn: "○", danger: "■", info: "■" }[check.tone];
  return (
    <Dialog.Root open onOpenChange={(open: boolean) => !open && !remove.isPending && onClose()}>
      <Dialog className="hx-dialog" size="base">
        <Dialog.Title className="hx-dialog-title">{t("account.factors.removeTitle", { name: row.name })}</Dialog.Title>
        <div className="hx-dialog-body">
          <div className={`hx-check hx-check--${check.tone}`}>
            <span className="hx-check-shape" aria-hidden="true">
              {shape}
            </span>
            <div>
              <strong>{check.title}</strong>
              <span>{check.detail}</span>
            </div>
          </div>
          <AuthError error={reported(remove.error)} messages={{ LAST_SECOND_FACTOR: "account.factors.lastRequired" }} />
        </div>
        <div className="hx-dialog-actions">
          <Button variant="secondary" onClick={onClose} disabled={remove.isPending}>
            {t("auth.cancel")}
          </Button>
          <Button
            variant="destructive"
            onClick={() => remove.mutate()}
            disabled={remove.isPending || (last && required)}
          >
            {t("account.factors.remove")}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function CodesDialog({ codes, onClose }: { codes: string[]; onClose: () => void }) {
  const { t } = useTranslation();
  const session = useSession();
  const account = session.status === "signed-in" ? session.session.user.username : "";
  return (
    <Dialog.Root open disablePointerDismissal onOpenChange={(open: boolean) => !open && onClose()}>
      <Dialog className="hx-dialog" size="base">
        <Dialog.Title className="hx-dialog-title">{t("auth.codes.title")}</Dialog.Title>
        <div className="hx-dialog-body">
          <RecoveryCodes codes={codes} account={account} continueLabel={t("account.done")} onContinue={onClose} />
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function RecoveryPanel() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const reauth = useReauth();
  const factors = useQuery({ queryKey: authKeys.factors, queryFn: authApi.factors });
  const [codes, setCodes] = useState<string[] | null>(null);
  const regenerate = useMutation({
    mutationFn: () => reauth(() => authApi.regenerateRecoveryCodes(), t("account.codes.regenerateAction")),
    onSuccess: (r) => {
      setCodes(r.recoveryCodes);
      void client.invalidateQueries({ queryKey: authKeys.factors });
    },
  });
  const f = factors.data;
  const enrolled = !!f && (!!f.totp?.confirmed || f.passkeys.length > 0);
  const left = f?.recoveryCodesRemaining ?? 0;
  const tone: StatusTone = left === 0 ? "danger" : left <= 3 ? "warn" : "ok";
  return (
    <Panel id="hx-codes" title={t("account.codes.title")}>
      <PanelBody>
        {f && !enrolled && <p className="hx-auth-muted">{t("account.codes.needFactor")}</p>}
        {f && enrolled && (
          <div className="hx-auth-row">
            <StatusPill tone={tone}>{t("account.codes.left", { left, total: RECOVERY_CODES_PER_SET })}</StatusPill>
            <span className="hx-auth-muted">
              {t("account.codes.used", { count: Math.max(0, RECOVERY_CODES_PER_SET - left) })}
            </span>
          </div>
        )}
        <AuthError error={reported(regenerate.error)} />
        <div>
          <Button
            variant="secondary"

            disabled={!enrolled || regenerate.isPending}
            onClick={() => regenerate.mutate()}
          >
            {t("account.codes.regenerate")}
          </Button>
        </div>
      </PanelBody>
      {codes && <CodesDialog codes={codes} onClose={() => setCodes(null)} />}
    </Panel>
  );
}

function deviceName(s: SessionInfo, unknown: string): string {
  return describeUserAgent(s.userAgent) ?? (s.userAgent.trim() || unknown);
}

function SessionsPanel() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const sessions = useQuery({ queryKey: authKeys.sessions, queryFn: authApi.sessions });
  const now = useNow(60_000);
  const refresh = () => client.invalidateQueries({ queryKey: authKeys.sessions });
  const revoke = useMutation({ mutationFn: (id: string) => authApi.revokeSession(id), onSettled: refresh });
  const revokeOthers = useMutation({ mutationFn: () => authApi.revokeOtherSessions(), onSettled: refresh });
  const list = [...(sessions.data?.sessions ?? [])].sort(
    (a, b) => Number(b.current) - Number(a.current) || b.lastSeenAt.localeCompare(a.lastSeenAt),
  );
  const others = list.filter((s) => !s.current).length;
  return (
    <Panel
      id="hx-sessions"
      title={sessions.data ? t("account.sessions.titleCount", { count: list.length }) : t("account.sessions.title")}
      actions={
        <Button
          variant="secondary"

          disabled={others === 0 || revokeOthers.isPending}
          onClick={() => revokeOthers.mutate()}
        >
          {t("account.sessions.revokeOthers")}
        </Button>
      }
    >
      {sessions.isError ? (
        <PanelBody>
          <AuthError error={sessions.error} />
        </PanelBody>
      ) : (
        <div className="hx-table-wrap">
          <Table className="hx-table">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("account.sessions.device")}</Table.Head>
                <Table.Head>{t("account.sessions.status")}</Table.Head>
                <Table.Head>{t("account.sessions.lastActive")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {list.map((s) => {
                const name = deviceName(s, t("account.sessions.unknownDevice"));
                return (
                  <Table.Row key={s.id}>
                    <Table.Cell>
                      <div className="hx-name">{name}</div>
                      <div className="hx-sub hx-mono">{s.sourceIp}</div>
                    </Table.Cell>
                    <Table.Cell>
                      {s.current ? (
                        <StatusPill tone="ok">{t("account.sessions.current")}</StatusPill>
                      ) : (
                        <Button
                          variant="secondary"

                          aria-label={t("account.sessions.revokeNamed", { name, ip: s.sourceIp })}
                          disabled={revoke.isPending}
                          onClick={() => revoke.mutate(s.id)}
                        >
                          {t("account.sessions.revoke")}
                        </Button>
                      )}
                    </Table.Cell>
                    <Table.Cell className="hx-nowrap">
                      {formatRelative(s.lastSeenAt, now, activeLocale(), t("account.justNow"))}
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </div>
      )}
      {(revoke.error || revokeOthers.error) && (
        <PanelBody>
          <AuthError error={revoke.error ?? revokeOthers.error} />
        </PanelBody>
      )}
    </Panel>
  );
}

/** Idle timeouts offered, in seconds; Core accepts 15 minutes to 24 hours (SessionSettings bounds). */
const IDLE_CHOICES = [900, 1800, 3600, 7200, 14400, 28800, 43200, 86400];

function useDurationLabel() {
  const { t } = useTranslation();
  return (secs: number) =>
    secs % 3600 === 0
      ? t("account.idle.hours", { count: secs / 3600 })
      : t("account.idle.minutes", { count: Math.round(secs / 60) });
}

function IdlePanel() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const label = useDurationLabel();
  const settings = useQuery({ queryKey: authKeys.sessionSettings, queryFn: authApi.sessionSettings });
  const save = useMutation({
    mutationFn: (secs: number) => authApi.putSessionSettings(secs),
    onSuccess: (s: SessionSettings) => {
      client.setQueryData(authKeys.sessionSettings, s);
      void client.invalidateQueries({ queryKey: authKeys.sessions });
    },
  });
  const s = settings.data;
  const items = s
    ? [
        {
          value: 0,
          label: s.default
            ? t("account.idle.defaultIs", { duration: label(s.idleTimeoutSeconds) })
            : t("account.idle.default"),
        },
        ...[...new Set([...IDLE_CHOICES, ...(s.default ? [] : [s.idleTimeoutSeconds])])]
          .filter((v) => v >= s.minSeconds && v <= s.maxSeconds && !(s.default && v === s.idleTimeoutSeconds))
          .sort((a, b) => a - b)
          .map((v) => ({ value: v, label: label(v) })),
      ]
    : [];
  const value = s ? (s.default ? 0 : s.idleTimeoutSeconds) : 0;
  return (
    <Panel id="hx-idle" title={t("account.idle.title")}>
      <PanelBody>
        {settings.isError && <AuthError error={settings.error} />}
        {s && (
          <Select
            className="hx-idle-select"
            label={t("account.idle.label")}
            items={items}
            value={value}
            disabled={save.isPending}
            onValueChange={(v: unknown) => {
              if (typeof v === "number" && v !== value) save.mutate(v);
            }}
          />
        )}
        <AuthError error={save.error} />
      </PanelBody>
    </Panel>
  );
}

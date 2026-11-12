import { Table } from "@cloudflare/kumo/components/table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { isApiError } from "../auth/api";
import { Notice } from "../auth/parts";
import { StatusPill, type StatusTone } from "../components/StatusPill";
import { PageHead } from "../shell/PageShell";
import { PageSkeleton, StateBlock } from "../shell/PageStates";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { NODE_STATE, nodeKeys, nodesApi, type Node } from "./api";
import { REFRESH_MS, useCanWriteNodes } from "./hooks";
import { NodeActionDialog } from "./NodeActions";
import { NODES_PATH } from "./paths";
import { CertificatePill, Field, HostStatusPill, LastContact, NodeError, PendingSync, WriteButton } from "./parts";
import { at, availableActions, hostStatusNote, type NodeAction } from "./status";
import "./nodes.css";

/**
 * One host: what Core knows about it and the actions it allows (ticket 125). The host workspace tabs
 * (monitoring, system, files, terminal, disks) belong to later tickets.
 */
export function NodePage() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const q = useQuery({
    queryKey: nodeKeys.one(id),
    queryFn: () => nodesApi.get(id),
    refetchInterval: REFRESH_MS,
  });
  const n = q.data?.node;
  useDocumentTitle(n ? n.name || n.hostname || n.id : t("nodes.title"));
  const crumbs = (
    <nav className="hx-crumbs" aria-label={t("nodes.breadcrumb")}>
      <Link to={NODES_PATH}>{t("nodes.title")}</Link>
      <span aria-hidden="true"> / </span>
    </nav>
  );
  if (q.isError && !n) {
    return (
      <>
        {crumbs}
        <StateBlock
          heading
          title={isApiError(q.error, "NOT_FOUND") ? t("nodes.detail.notFound", { id }) : t("nodes.detail.loadFailed")}
          detail={isApiError(q.error, "NOT_FOUND") ? undefined : <NodeError error={q.error} />}
          actions={
            <Link to={NODES_PATH} className="hx-btn-link">
              {t("nodes.detail.backToList")}
            </Link>
          }
        />
      </>
    );
  }
  if (!n) return <PageSkeleton delayMs={400} />;
  return (
    <>
      {crumbs}
      <NodeDetail node={n} />
    </>
  );
}

function NodeDetail({ node: n }: { node: Node }) {
  const { t } = useTranslation();
  const canWrite = useCanWriteNodes(n.id);
  const [open, setOpen] = useState<NodeAction | null>(null);
  const name = n.name || n.hostname || n.id;
  const note = hostStatusNote(n, t);
  const actions = availableActions(n);
  return (
    <>
      <PageHead
        title={name}
        actions={actions.map((a) => (
          <WriteButton
            key={a}
            allowed={canWrite}
            variant={a === "revoke" || a === "remove" ? "secondary-destructive" : "secondary"}
            onClick={() => setOpen(a)}
          >
            {t(`nodes.action.${a}`)}
          </WriteButton>
        ))}
      />
      <div className="hx-node-status">
        <HostStatusPill node={n} />
        {note && <span className="hx-note">{note}</span>}
        <span className="hx-note">
          {t("nodes.detail.lastContact")} <LastContact node={n} />
        </span>
      </div>
      {n.state === NODE_STATE.removed && n.removal && (
        <Notice tone="info">
          {t("nodes.detail.removed", {
            mode: t(`nodes.remove.mode.${n.removal.mode}`),
            time: at(n.removal.at),
            by: n.removal.by,
          })}
        </Notice>
      )}
      {n.state === NODE_STATE.conflict && (
        <Notice tone="danger">{t("nodes.detail.conflict", { time: at(n.conflictAt) })}</Notice>
      )}
      <div className="hx-node-grid">
        <IdentityPanel node={n} />
        <ConnectionPanel node={n} />
        <CertificatePanel node={n} />
        <MaintenancePanel node={n} canWrite={canWrite} />
        {n.revocation && <RevocationPanel node={n} />}
        <ComponentsPanel node={n} />
      </div>
      {open && <NodeActionDialog node={n} action={open} onClose={() => setOpen(null)} />}
    </>
  );
}

function Panel({
  id,
  title,
  children,
  extra,
}: {
  id: string;
  title: string;
  children: React.ReactNode;
  extra?: React.ReactNode;
}) {
  return (
    <section className="hx-panel" aria-labelledby={id}>
      <div className="hx-panel-head">
        <h2 id={id}>{title}</h2>
        {extra}
      </div>
      <div className="hx-panel-body">{children}</div>
    </section>
  );
}

function IdentityPanel({ node: n }: { node: Node }) {
  const { t } = useTranslation();
  return (
    <Panel id="hx-node-identity" title={t("nodes.detail.identity")}>
      <dl className="hx-fields">
        <Field label={t("nodes.fields.hostname")} mono>
          {n.hostname || "—"}
        </Field>
        <Field label={t("nodes.fields.os")}>{[n.os, n.arch].filter(Boolean).join(" · ") || "—"}</Field>
        <Field label={t("nodes.fields.group")}>{n.group || "—"}</Field>
        <Field label={t("nodes.fields.tags")}>{(n.tags ?? []).join(t("nodes.listSeparator")) || "—"}</Field>
        <Field label={t("nodes.fields.machineId")} mono>
          {n.machineId || "—"}
        </Field>
        <Field label={t("nodes.fields.fingerprint")} mono>
          {n.fingerprint}
        </Field>
        <Field label={t("nodes.fields.id")} mono>
          {n.id}
        </Field>
        <Field label={t("nodes.fields.createdAt")}>{at(n.createdAt)}</Field>
      </dl>
    </Panel>
  );
}

function ConnectionPanel({ node: n }: { node: Node }) {
  const { t } = useTranslation();
  const c = n.connection;
  return (
    <Panel id="hx-node-connection" title={t("nodes.detail.connection")}>
      <dl className="hx-fields">
        <Field label={t("nodes.col.status")}>
          <HostStatusPill node={n} />
        </Field>
        <Field label={t("nodes.col.lastContact")}>{at(c.lastContactAt) ?? t("nodes.noContact")}</Field>
        <Field label={t("nodes.detail.connectedAt")}>{at(c.connectedAt) ?? "—"}</Field>
        <Field label={t("nodes.detail.remoteAddr")} mono>
          {c.remoteAddr || "—"}
        </Field>
        <Field label={t("nodes.detail.clockSkew")}>
          {c.clockSkewSeconds == null ? (
            "—"
          ) : c.clockSkewWarning ? (
            <StatusPill tone="warn">{t("nodes.detail.skew", { count: c.clockSkewSeconds })}</StatusPill>
          ) : (
            t("nodes.detail.skew", { count: c.clockSkewSeconds })
          )}
        </Field>
        <Field label={t("nodes.col.pending")}>
          <PendingSync node={n} />
        </Field>
      </dl>
    </Panel>
  );
}

function CertificatePanel({ node: n }: { node: Node }) {
  const { t } = useTranslation();
  const c = n.certificate;
  const r = c.rotation;
  return (
    <Panel id="hx-node-cert" title={t("nodes.detail.certificate")} extra={<CertificatePill node={n} />}>
      <dl className="hx-fields">
        <Field label={t("nodes.detail.serial")} mono>
          {c.serial}
        </Field>
        <Field label={t("nodes.detail.notAfter")}>
          {t("nodes.detail.notAfterValue", { date: at(c.notAfter), count: c.expiresInDays })}
        </Field>
        {r && (
          <>
            <Field label={t("nodes.detail.newSerial")} mono>
              {r.newSerial}
            </Field>
            <Field label={t("nodes.detail.rotationRequested")}>{at(r.requestedAt)}</Field>
            <Field label={t("nodes.detail.rotationDelivered")}>{at(r.deliveredAt) ?? t("nodes.detail.notYet")}</Field>
          </>
        )}
      </dl>
      {r && !r.error && <p className="hx-note">{t("nodes.detail.rotationBoth")}</p>}
      {r?.error && <Notice tone="danger">{t("nodes.detail.rotationError", { error: r.error })}</Notice>}
    </Panel>
  );
}

const RECHECK_TONE: Record<string, StatusTone> = {
  pending: "warn",
  checking: "info",
  passed: "ok",
  failed: "danger",
  inconclusive: "warn",
};

function MaintenancePanel({ node: n, canWrite }: { node: Node; canWrite: boolean | undefined }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const m = n.maintenance;
  const r = m.recheck;
  const recheck = useMutation({
    mutationFn: () => nodesApi.recheck(n.id),
    onSuccess: (res) => {
      client.setQueryData(nodeKeys.one(n.id), res);
      void client.invalidateQueries({ queryKey: nodeKeys.all });
    },
  });
  if (!m.enabled && !m.endedAt && !r) {
    return (
      <Panel id="hx-node-maint" title={t("nodes.detail.maintenance")}>
        <p className="hx-note">{t("nodes.detail.notInMaintenance")}</p>
      </Panel>
    );
  }
  return (
    <Panel
      id="hx-node-maint"
      title={t("nodes.detail.maintenance")}
      extra={m.enabled ? <StatusPill tone="info">{t("nodes.status.maintenance")}</StatusPill> : undefined}
    >
      <dl className="hx-fields">
        {m.enabled && <Field label={t("nodes.detail.since")}>{at(m.since)}</Field>}
        {m.reason && <Field label={t("nodes.maintenance.reason")}>{m.reason}</Field>}
        {m.endedAt && <Field label={t("nodes.detail.endedAt")}>{at(m.endedAt)}</Field>}
        {m.checkedAt && <Field label={t("nodes.detail.reconnectedAt")}>{at(m.checkedAt)}</Field>}
        {r && (
          <Field label={t("nodes.detail.recheck")}>
            <StatusPill tone={RECHECK_TONE[r.state] ?? "warn"}>
              {t(`nodes.recheck.${r.state}`, { defaultValue: r.state })}
            </StatusPill>
          </Field>
        )}
        {r?.finishedAt && <Field label={t("nodes.detail.recheckFinished")}>{at(r.finishedAt)}</Field>}
        {r && r.state !== "pending" && r.state !== "checking" && (
          <Field label={t("nodes.detail.identityMatches")}>
            {r.identityMatches ? t("nodes.detail.yes") : t("nodes.detail.no")}
          </Field>
        )}
      </dl>
      {r?.detail && (
        <pre className="hx-raw-pre" translate="no">
          {r.detail}
        </pre>
      )}
      {r?.health != null && <HealthValues health={r.health as Record<string, unknown>} />}
      {r && (r.state === "failed" || r.state === "inconclusive") && n.state === NODE_STATE.active && (
        <>
          <NodeError error={recheck.error} />
          <WriteButton
            allowed={canWrite}
            variant="secondary"
            onClick={() => recheck.mutate()}
            disabled={recheck.isPending}
          >
            {t("nodes.detail.recheckAgain")}
          </WriteButton>
        </>
      )}
    </Panel>
  );
}

/** The host values the re-check read, as the agent reported them; unreadable ones stay "—". */
function HealthValues({ health }: { health: Record<string, unknown> }) {
  const { t } = useTranslation();
  const entries = Object.entries(health);
  if (entries.length === 0) return null;
  return (
    <details className="hx-raw-details">
      <summary className="hx-raw-details-summary">{t("nodes.detail.healthValues")}</summary>
      <pre className="hx-raw-pre" translate="no">
        {JSON.stringify(health, null, 2)}
      </pre>
    </details>
  );
}

function RevocationPanel({ node: n }: { node: Node }) {
  const { t } = useTranslation();
  const r = n.revocation!;
  return (
    <Panel id="hx-node-revocation" title={t("nodes.detail.revocation")}>
      <dl className="hx-fields">
        <Field label={t("nodes.detail.revokedAt")}>{at(r.requestedAt)}</Field>
        <Field label={t("nodes.detail.revokedBy")}>{r.by || "—"}</Field>
        <Field label={t("nodes.revoke.reason")}>{r.reason || "—"}</Field>
        <Field label={t("nodes.detail.sessionClosed")}>{at(r.sessionClosedAt) ?? t("nodes.detail.noSession")}</Field>
        <Field label={t("nodes.detail.agentNotified")}>
          {r.pending ? (
            <StatusPill tone="warn">{t("nodes.detail.revocationPending")}</StatusPill>
          ) : (
            (at(r.agentNotifiedAt) ?? "—")
          )}
        </Field>
        {r.propagationSeconds != null && (
          <Field label={t("nodes.detail.propagation")}>
            {t("nodes.detail.seconds", { count: r.propagationSeconds })}
          </Field>
        )}
      </dl>
    </Panel>
  );
}

function ComponentsPanel({ node: n }: { node: Node }) {
  const { t } = useTranslation();
  const list = n.components ?? [];
  return (
    <section className="hx-panel hx-node-wide" aria-labelledby="hx-node-components">
      <div className="hx-panel-head">
        <h2 id="hx-node-components">{t("nodes.detail.components")}</h2>
      </div>
      {list.length === 0 ? (
        <div className="hx-panel-body">
          <p className="hx-note">{t("nodes.detail.noComponents")}</p>
        </div>
      ) : (
        <div className="hx-table-wrap">
          <Table className="hx-table">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("nodes.detail.component")}</Table.Head>
                <Table.Head>{t("nodes.detail.version")}</Table.Head>
                <Table.Head>{t("nodes.col.status")}</Table.Head>
                <Table.Head>{t("nodes.detail.source")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {list.map((c) => (
                <Table.Row key={`${c.kind}:${c.name}`}>
                  <Table.Cell>
                    <div className="hx-name">{c.name}</div>
                    <div className="hx-sub">{t(`nodes.component.${c.kind}`, { defaultValue: c.kind })}</div>
                  </Table.Cell>
                  <Table.Cell className="hx-mono">{c.version || "—"}</Table.Cell>
                  <Table.Cell>
                    <span className="hx-mono">{c.state}</span>
                  </Table.Cell>
                  <Table.Cell className="hx-sub">{c.source}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
    </section>
  );
}

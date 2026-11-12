import { LinkButton } from "@cloudflare/kumo/components/button";
import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Table } from "@cloudflare/kumo/components/table";
import { DotsThreeIcon, LockIcon, MagnifyingGlassIcon, PlusIcon } from "@phosphor-icons/react";
import { useEffect, useMemo, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router";
import { ApiErrorMessage } from "../components/RawMessage";
import type { ApiError } from "../api/client";
import { PageHead } from "../shell/PageShell";
import { PageSkeleton, StateBlock } from "../shell/PageStates";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import type { Node } from "./api";
import { useCanWriteNodes, useNodeList } from "./hooks";
import { NodeActionDialog } from "./NodeActions";
import { CertificatePill, HostStatusPill, LastContact, PendingSync } from "./parts";
import {
  availableActions,
  apiState,
  certificateStatus,
  hasFilters,
  isUnreachable,
  matchesStatus,
  readFilters,
  STATUS_FILTERS,
  tagsAndGroups,
  writeFilters,
  hostStatusNote,
  type HostFilters,
  type NodeAction,
} from "./status";
import { ADD_NODE_PATH, nodeHref } from "./paths";
import "./nodes.css";

/** Host list (board Nodes「主機」): summary, filters written into the URL, the table and its row actions. */
export function NodesPage() {
  const { t } = useTranslation();
  useDocumentTitle(t("nodes.title"));
  const [params, setParams] = useSearchParams();
  const filters = readFilters(params);
  const canWrite = useCanWriteNodes();
  const all = useNodeList();
  const filtered = useNodeList(
    {
      q: filters.q.trim() || undefined,
      tag: filters.tag || undefined,
      group: filters.group || undefined,
      state: apiState(filters.status),
    },
    hasFilters(filters),
  );
  const source = hasFilters(filters) ? filtered : all;
  const rows = useMemo(
    () => (source.data?.items ?? []).filter((n) => matchesStatus(n, filters.status)),
    [source.data, filters.status],
  );
  const setFilters = (f: HostFilters) => setParams(writeFilters(f), { replace: true });

  const addButton = <AddButton allowed={canWrite} label={t("nodes.add")} />;

  return (
    <>
      <PageHead title={t("nodes.title")} actions={addButton} />
      {all.isError && !all.data ? (
        <ApiErrorMessage error={all.error as ApiError} />
      ) : !all.data ? (
        <PageSkeleton delayMs={400} />
      ) : all.data.items.length === 0 && !hasFilters(filters) ? (
        <StateBlock
          title={t("nodes.empty.title")}
          detail={<p className="hx-note">{t("nodes.empty.detail")}</p>}
          actions={addButton}
        />
      ) : (
        <>
          <Summary nodes={all.data.items} />
          <section className="hx-panel" aria-labelledby="hx-hosts-title">
            <div className="hx-panel-head">
              <h2 id="hx-hosts-title">{t("nodes.listTitle", { count: rows.length })}</h2>
            </div>
            <Filters filters={filters} onChange={setFilters} nodes={all.data.items} />
            {source.isError ? (
              <div className="hx-panel-body">
                <ApiErrorMessage error={source.error as ApiError} />
              </div>
            ) : !source.data ? (
              <div className="hx-panel-body">
                <PageSkeleton delayMs={400} />
              </div>
            ) : rows.length === 0 ? (
              <div className="hx-panel-body">
                <StateBlock
                  title={t("nodes.noMatch.title")}
                  detail={<p className="hx-note">{describeFilters(filters, t)}</p>}
                  actions={
                    <Button variant="secondary" onClick={() => setFilters(readFilters(new URLSearchParams()))}>
                      {t("nodes.noMatch.clear")}
                    </Button>
                  }
                />
              </div>
            ) : (
              <HostTable rows={rows} canWrite={canWrite} />
            )}
            {source.data && !source.data.complete && (
              <p className="hx-note hx-panel-note">
                {t("nodes.capped", { shown: source.data.items.length, total: source.data.page.total ?? "?" })}
              </p>
            )}
          </section>
        </>
      )}
    </>
  );
}

function AddButton({ allowed, label }: { allowed: boolean | undefined; label: string }) {
  const { t } = useTranslation();
  if (allowed === false) {
    return (
      <Button variant="primary" disabled icon={<LockIcon aria-hidden="true" />} title={t("nodes.needWrite")}>
        {label}
        <span className="hx-visually-hidden">{t("nodes.needWrite")}</span>
      </Button>
    );
  }
  return (
    <LinkButton href={ADD_NODE_PATH} variant="primary" icon={<PlusIcon aria-hidden="true" />}>
      {label}
    </LinkButton>
  );
}

function describeFilters(f: HostFilters, t: TFunction): string {
  const parts: string[] = [];
  if (f.q.trim()) parts.push(t("nodes.filter.describeQ", { q: f.q.trim() }));
  if (f.status !== "all") parts.push(t(`nodes.filter.status.${f.status}`));
  if (f.tag) parts.push(t("nodes.filter.describeTag", { tag: f.tag }));
  if (f.group) parts.push(t("nodes.filter.describeGroup", { group: f.group }));
  return t("nodes.noMatch.detail", { conditions: parts.join(t("nodes.listSeparator")) });
}

/** Stat strip (DESIGN.md stat strip): hosts by state, certificates that need attention, changes not delivered. */
function Summary({ nodes }: { nodes: Node[] }) {
  const { t } = useTranslation();
  const unreachable = nodes.filter(isUnreachable).length;
  const maintenance = nodes.filter((n) => n.state === "active" && n.maintenance.enabled).length;
  const trust = nodes.filter((n) => n.state === "revoked" || n.state === "identity_conflict").length;
  const certs = nodes.filter((n) => n.state === "active" && certificateStatus(n).tone !== "ok").length;
  const pending = nodes.filter((n) => (n.pendingSync ?? []).length > 0).length;
  return (
    <dl className="hx-panel hx-strip">
      <div className="hx-strip-cell">
        <dt>{t("nodes.summary.hosts")}</dt>
        <dd className="hx-strip-value">{nodes.length}</dd>
        <dd className="hx-strip-detail">{t("nodes.summary.hostsDetail", { unreachable, maintenance, trust })}</dd>
      </div>
      <div className="hx-strip-cell">
        <dt>{t("nodes.summary.certificates")}</dt>
        <dd className="hx-strip-value">{certs}</dd>
        <dd className="hx-strip-detail">{t("nodes.summary.certificatesDetail")}</dd>
      </div>
      <div className="hx-strip-cell">
        <dt>{t("nodes.summary.pending")}</dt>
        <dd className="hx-strip-value">{pending}</dd>
        <dd className="hx-strip-detail">{t("nodes.summary.pendingDetail")}</dd>
      </div>
    </dl>
  );
}

function Filters({
  filters,
  onChange,
  nodes,
}: {
  filters: HostFilters;
  onChange: (f: HostFilters) => void;
  nodes: Node[];
}) {
  const { t } = useTranslation();
  const { tags, groups } = useMemo(() => tagsAndGroups(nodes), [nodes]);
  // The search box writes to the URL after a short pause, so typing does not add a request per key.
  const [q, setQ] = useState(filters.q);
  const [urlQ, setUrlQ] = useState(filters.q);
  if (urlQ !== filters.q) {
    // The URL changed from outside (clear filters, back button): show what it says now.
    setUrlQ(filters.q);
    setQ(filters.q);
  }
  useEffect(() => {
    if (q === filters.q) return;
    const id = window.setTimeout(() => onChange({ ...filters, q }), 300);
    return () => window.clearTimeout(id);
  }, [q, filters, onChange]);
  const any = t("nodes.filter.any");
  return (
    <div className="hx-filters">
      <div className="hx-filter-search">
        <Input
          label={t("nodes.filter.search")}
          placeholder={t("nodes.filter.searchHint")}
          value={q}
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setQ(e.target.value)}
        />
        <MagnifyingGlassIcon size={16} aria-hidden="true" className="hx-filter-search-icon" />
      </div>
      <div className="hx-chips" role="group" aria-label={t("nodes.filter.statusLabel")}>
        {STATUS_FILTERS.map((s) => (
          <button
            key={s}
            type="button"
            className="hx-chip"
            aria-pressed={filters.status === s}
            onClick={() => onChange({ ...filters, status: s })}
          >
            {t(`nodes.filter.status.${s}`)}
          </button>
        ))}
      </div>
      <Select
        className="hx-filter-select"
        label={t("nodes.filter.tag")}
        items={[{ value: "", label: any }, ...tags.map((v) => ({ value: v, label: v }))]}
        value={filters.tag}
        onValueChange={(v: unknown) => onChange({ ...filters, tag: typeof v === "string" ? v : "" })}
      />
      <Select
        className="hx-filter-select"
        label={t("nodes.filter.group")}
        items={[{ value: "", label: any }, ...groups.map((v) => ({ value: v, label: v }))]}
        value={filters.group}
        onValueChange={(v: unknown) => onChange({ ...filters, group: typeof v === "string" ? v : "" })}
      />
    </div>
  );
}

function osLine(n: Node): string {
  return [n.hostname, [n.os, n.arch].filter(Boolean).join(" "), n.group].filter(Boolean).join(" · ");
}

function HostTable({ rows, canWrite }: { rows: Node[]; canWrite: boolean | undefined }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState<{ node: Node; action: NodeAction } | null>(null);
  return (
    <div className="hx-table-wrap">
      <Table className="hx-table hx-hosts">
        <Table.Header>
          <Table.Row>
            <Table.Head>{t("nodes.col.host")}</Table.Head>
            <Table.Head>{t("nodes.col.status")}</Table.Head>
            <Table.Head>{t("nodes.col.lastContact")}</Table.Head>
            <Table.Head className="c5">{t("nodes.col.certificate")}</Table.Head>
            <Table.Head className="c5">{t("nodes.col.pending")}</Table.Head>
            <Table.Head>
              <span className="hx-visually-hidden">{t("nodes.col.actions")}</span>
            </Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.map((n) => {
            const note = hostStatusNote(n, t);
            const name = n.name || n.hostname || n.id;
            const actions = availableActions(n);
            return (
              <Table.Row key={n.id}>
                <Table.Cell>
                  <Link to={nodeHref(n.id)} className="hx-name hx-row-link">
                    {name}
                  </Link>
                  <div className="hx-sub">{osLine(n) || t("nodes.noHostInfo")}</div>
                  {(n.tags ?? []).length > 0 && (
                    <ul className="hx-tags" aria-label={t("nodes.fields.tags")}>
                      {(n.tags ?? []).map((tag) => (
                        <li key={tag}>{tag}</li>
                      ))}
                    </ul>
                  )}
                </Table.Cell>
                <Table.Cell>
                  <HostStatusPill node={n} />
                  {note && <div className="hx-sub">{note}</div>}
                </Table.Cell>
                <Table.Cell className="hx-nowrap">
                  <LastContact node={n} />
                </Table.Cell>
                <Table.Cell className="c5">
                  <CertificatePill node={n} />
                </Table.Cell>
                <Table.Cell className="c5">
                  <PendingSync node={n} />
                </Table.Cell>
                <Table.Cell className="hx-acts">
                  {actions.length > 0 && (
                    <DropdownMenu>
                      <DropdownMenu.Trigger
                        render={
                          <button
                            type="button"
                            className="hx-ib hx-ib--line"
                            aria-label={t("nodes.moreFor", { name })}
                            title={t("nodes.more")}
                          >
                            <DotsThreeIcon size={18} aria-hidden="true" />
                          </button>
                        }
                      />
                      <DropdownMenu.Content align="end">
                        {actions.map((a) => (
                          <DropdownMenu.Item
                            key={a}
                            disabled={canWrite !== true}
                            onClick={() => setOpen({ node: n, action: a })}
                          >
                            {canWrite === false && <LockIcon size={14} aria-hidden="true" />}
                            {t(`nodes.action.${a}`)}
                          </DropdownMenu.Item>
                        ))}
                        {canWrite === false && (
                          <div className="hx-menu-note" role="note">
                            {t("nodes.needWrite")}
                          </div>
                        )}
                      </DropdownMenu.Content>
                    </DropdownMenu>
                  )}
                </Table.Cell>
              </Table.Row>
            );
          })}
        </Table.Body>
      </Table>
      {open && <NodeActionDialog node={open.node} action={open.action} onClose={() => setOpen(null)} />}
    </div>
  );
}

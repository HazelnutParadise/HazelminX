import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Input } from "@cloudflare/kumo/components/input";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { isApiError } from "../auth/api";
import { Notice } from "../auth/parts";
import { StatusPill } from "../components/StatusPill";
import { nodeKeys, nodesApi, REMOVAL_MODES, type AffectedJob, type Node, type RemovalMode } from "./api";
import { NodeError } from "./parts";
import { at, parseTags, type NodeAction } from "./status";

export type { NodeAction };

function nameOf(n: Node): string {
  return n.name || n.hostname || n.id;
}

/** Refreshes every host list and this host after a change. */
function useRefresh() {
  const client = useQueryClient();
  return (n?: Node) => {
    if (n) client.setQueryData(nodeKeys.one(n.id), { node: n });
    return client.invalidateQueries({ queryKey: nodeKeys.all });
  };
}

/** Renders the dialog for `action`, or nothing. */
export function NodeActionDialog({
  node,
  action,
  onClose,
}: {
  node: Node;
  action: NodeAction | null;
  onClose: () => void;
}) {
  switch (action) {
    case "edit":
      return <EditDialog node={node} onClose={onClose} />;
    case "maintenanceOn":
      return <MaintenanceOnDialog node={node} onClose={onClose} />;
    case "maintenanceOff":
      return <MaintenanceOffDialog node={node} onClose={onClose} />;
    case "rotate":
      return <RotateDialog node={node} onClose={onClose} />;
    case "revoke":
      return <RevokeDialog node={node} onClose={onClose} />;
    case "remove":
      return <RemoveDialog node={node} onClose={onClose} />;
    default:
      return null;
  }
}

/**
 * Dialog frame. Confirmations close on the backdrop and Esc; dialogs with typed input do not close on the
 * backdrop, and Esc or the close button ask whether to drop the input (DESIGN.md 對話框關閉規則). Running
 * requests cannot be interrupted, so nothing closes while `busy`.
 */
function ActionDialog({
  title,
  children,
  actions,
  onClose,
  dirty = false,
  busy = false,
  onSaveAndClose,
}: {
  title: string;
  children: ReactNode;
  actions: ReactNode;
  onClose: () => void;
  dirty?: boolean;
  busy?: boolean;
  onSaveAndClose?: () => void;
}) {
  const { t } = useTranslation();
  const [asking, setAsking] = useState(false);
  const tryClose = () => {
    if (busy) return;
    if (dirty) setAsking(true);
    else onClose();
  };
  return (
    <Dialog.Root open disablePointerDismissal={dirty || busy} onOpenChange={(open: boolean) => !open && tryClose()}>
      <Dialog className="hx-dialog" size="base">
        <Dialog.Title className="hx-dialog-title">{title}</Dialog.Title>
        <div className="hx-dialog-body">{children}</div>
        {asking ? (
          <div className="hx-dialog-ask" role="group" aria-label={t("nodes.dialog.unsaved")}>
            <span>{t("nodes.dialog.unsaved")}</span>
            <div className="hx-dialog-actions">
              <Button variant="secondary" onClick={() => setAsking(false)}>
                {t("nodes.dialog.keepEditing")}
              </Button>
              <Button variant="secondary" onClick={onClose}>
                {t("nodes.dialog.discard")}
              </Button>
              {onSaveAndClose && (
                <Button
                  variant="primary"
                  onClick={() => {
                    setAsking(false);
                    onSaveAndClose();
                  }}
                >
                  {t("nodes.dialog.saveAndClose")}
                </Button>
              )}
            </div>
          </div>
        ) : (
          <div className="hx-dialog-actions">{actions}</div>
        )}
      </Dialog>
    </Dialog.Root>
  );
}

function EditDialog({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useTranslation();
  const refresh = useRefresh();
  const [base, setBase] = useState(node);
  const [name, setName] = useState(node.name);
  const [tags, setTags] = useState((node.tags ?? []).join(", "));
  const [group, setGroup] = useState(node.group);
  const patch = {
    ...(name.trim() !== base.name ? { name: name.trim() } : {}),
    ...(parseTags(tags).join("\n") !== (base.tags ?? []).join("\n") ? { tags: parseTags(tags) } : {}),
    ...(group.trim() !== base.group ? { group: group.trim() } : {}),
  };
  const dirty = Object.keys(patch).length > 0;
  const save = useMutation({
    mutationFn: () => nodesApi.update(base.id, base.version, patch),
    onSuccess: async (r) => {
      await refresh(r.node);
      onClose();
    },
  });
  const conflict = isApiError(save.error, "VERSION_MISMATCH");
  const reload = useMutation({
    mutationFn: () => nodesApi.get(base.id),
    onSuccess: (r) => {
      setBase(r.node);
      setName(r.node.name);
      setTags((r.node.tags ?? []).join(", "));
      setGroup(r.node.group);
      save.reset();
      void refresh(r.node);
    },
  });
  return (
    <ActionDialog
      title={t("nodes.edit.title", { name: nameOf(base) })}
      onClose={onClose}
      dirty={dirty}
      busy={save.isPending}
      onSaveAndClose={() => save.mutate()}
      actions={
        <>
          <Button variant="secondary" onClick={onClose} disabled={save.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button
            variant="primary"
            onClick={() => save.mutate()}
            disabled={!dirty || !name.trim() || save.isPending || conflict}
          >
            {t("nodes.edit.save")}
          </Button>
        </>
      }
    >
      <form
        className="hx-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (dirty && name.trim() && !conflict) save.mutate();
        }}
      >
        <Input
          label={t("nodes.fields.name")}
          value={name}
          required
          autoFocus
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
        />
        <Input
          label={t("nodes.fields.tags")}
          description={t("nodes.fields.tagsHint")}
          value={tags}
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setTags(e.target.value)}
        />
        <Input
          label={t("nodes.fields.group")}
          value={group}
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setGroup(e.target.value)}
        />
        <button type="submit" hidden />
      </form>
      {conflict ? (
        <div className="hx-dialog-conflict">
          <Notice tone="warn">{t("nodes.errors.versionMismatch")}</Notice>
          <Button variant="secondary" onClick={() => reload.mutate()} disabled={reload.isPending}>
            {t("nodes.edit.reload")}
          </Button>
          <NodeError error={reload.error} />
        </div>
      ) : (
        <NodeError error={save.error} />
      )}
    </ActionDialog>
  );
}

function JobList({ jobs }: { jobs: AffectedJob[] }) {
  const { t } = useTranslation();
  if (jobs.length === 0) return <p className="hx-muted">{t("nodes.maintenance.noJobs")}</p>;
  return (
    <ul className="hx-job-list" aria-label={t("nodes.maintenance.jobs")}>
      {jobs.map((j) => (
        <li key={j.id}>
          <Link to={`/jobs/${encodeURIComponent(j.id)}`} className="hx-mono">
            {j.kind}
          </Link>
          <StatusPill tone="info">{t(`nodes.jobState.${j.state}`, { defaultValue: j.state })}</StatusPill>
          <span className="hx-sub hx-mono">{j.id}</span>
        </li>
      ))}
    </ul>
  );
}

function MaintenanceOnDialog({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useTranslation();
  const refresh = useRefresh();
  const [reason, setReason] = useState("");
  const [jobs, setJobs] = useState<AffectedJob[] | null>(null);
  const start = useMutation({
    mutationFn: () => nodesApi.maintenance(node.id, true, reason.trim()),
    onSuccess: async (r) => {
      setJobs(r.affectedJobs ?? []);
      await refresh(r.node);
    },
  });
  const name = nameOf(node);
  if (jobs) {
    return (
      <ActionDialog
        title={t("nodes.maintenance.startedTitle", { name })}
        onClose={onClose}
        actions={
          <Button variant="primary" onClick={onClose}>
            {t("nodes.done")}
          </Button>
        }
      >
        <p>{t("nodes.maintenance.started")}</p>
        <h3 className="hx-dialog-h3">{t("nodes.maintenance.jobsCount", { count: jobs.length })}</h3>
        <JobList jobs={jobs} />
      </ActionDialog>
    );
  }
  return (
    <ActionDialog
      title={t("nodes.maintenance.startTitle", { name })}
      onClose={onClose}
      dirty={reason.trim() !== ""}
      busy={start.isPending}
      actions={
        <>
          <Button variant="secondary" onClick={onClose} disabled={start.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button variant="primary" onClick={() => start.mutate()} disabled={start.isPending}>
            {t("nodes.maintenance.start", { name })}
          </Button>
        </>
      }
    >
      <ul className="hx-consequences">
        <li>{t("nodes.maintenance.effectNew")}</li>
        <li>{t("nodes.maintenance.effectRunning")}</li>
        <li>{t("nodes.maintenance.effectBar")}</li>
      </ul>
      <Input
        label={t("nodes.maintenance.reason")}
        value={reason}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) => setReason(e.target.value)}
      />
      <NodeError error={start.error} />
    </ActionDialog>
  );
}

function MaintenanceOffDialog({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useTranslation();
  const refresh = useRefresh();
  const end = useMutation({
    mutationFn: () => nodesApi.maintenance(node.id, false, ""),
    onSuccess: async (r) => {
      await refresh(r.node);
      onClose();
    },
  });
  const name = nameOf(node);
  return (
    <ActionDialog
      title={t("nodes.maintenance.endTitle", { name })}
      onClose={onClose}
      busy={end.isPending}
      actions={
        <>
          <Button variant="secondary" onClick={onClose} disabled={end.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button variant="primary" onClick={() => end.mutate()} disabled={end.isPending}>
            {t("nodes.maintenance.end", { name })}
          </Button>
        </>
      }
    >
      <ul className="hx-consequences">
        <li>{t("nodes.maintenance.endEffectReconnect")}</li>
        <li>{t("nodes.maintenance.endEffectRecheck")}</li>
      </ul>
      <NodeError error={end.error} />
    </ActionDialog>
  );
}

function RotateDialog({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useTranslation();
  const refresh = useRefresh();
  const rotate = useMutation({
    mutationFn: () => nodesApi.rotate(node.id),
    onSuccess: async (r) => {
      await refresh(r.node);
      onClose();
    },
  });
  const name = nameOf(node);
  const online = node.connection.state === "online";
  return (
    <ActionDialog
      title={t("nodes.rotate.title", { name })}
      onClose={onClose}
      busy={rotate.isPending}
      actions={
        <>
          <Button variant="secondary" onClick={onClose} disabled={rotate.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button variant="primary" onClick={() => rotate.mutate()} disabled={rotate.isPending}>
            {t("nodes.rotate.confirm", { name })}
          </Button>
        </>
      }
    >
      <ul className="hx-consequences">
        <li>{t("nodes.rotate.effectBoth")}</li>
        <li>{online ? t("nodes.rotate.effectOnline") : t("nodes.rotate.effectOffline")}</li>
        <li>
          {t("nodes.rotate.effectCurrent", { serial: node.certificate.serial, date: at(node.certificate.notAfter) })}
        </li>
      </ul>
      <NodeError error={rotate.error} />
    </ActionDialog>
  );
}

function RevokeDialog({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useTranslation();
  const refresh = useRefresh();
  const [typed, setTyped] = useState("");
  const [reason, setReason] = useState("");
  const name = nameOf(node);
  const revoke = useMutation({
    mutationFn: () => nodesApi.revoke(node.id, reason.trim()),
    onSuccess: async (r) => {
      await refresh(r.node);
      onClose();
    },
  });
  const matches = typed.trim() === name;
  return (
    <ActionDialog
      title={t("nodes.revoke.title", { name })}
      onClose={onClose}
      dirty={typed !== "" || reason !== ""}
      busy={revoke.isPending}
      actions={
        <>
          <Button variant="secondary" onClick={onClose} disabled={revoke.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button variant="destructive" onClick={() => revoke.mutate()} disabled={!matches || revoke.isPending}>
            {t("nodes.revoke.confirm", { name })}
          </Button>
        </>
      }
    >
      <ul className="hx-consequences hx-consequences--danger">
        <li>{t("nodes.revoke.effectSession")}</li>
        <li>{t("nodes.revoke.effectReconnect")}</li>
        <li>{t("nodes.revoke.effectJobs")}</li>
        <li>{t("nodes.revoke.effectKey")}</li>
        {node.connection.state !== "online" && <li>{t("nodes.revoke.effectOffline")}</li>}
      </ul>
      <Input
        label={t("nodes.revoke.reason")}
        value={reason}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) => setReason(e.target.value)}
      />
      <Input
        label={t("nodes.revoke.typeName", { name })}
        value={typed}
        autoComplete="off"
        spellCheck={false}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) => setTyped(e.target.value)}
      />
      <NodeError error={revoke.error} />
    </ActionDialog>
  );
}

/** Which ticket brings each removal mode Core does not offer yet (POST /remove answers NOT_AVAILABLE). */
const MODE_TICKET: Record<Exclude<RemovalMode, "unmanage">, string[]> = {
  "remove-agent": ["037"],
  "remove-cluster-node": ["043", "087"],
  "delete-workloads": ["040", "044", "052"],
};

function RemoveDialog({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useTranslation();
  const refresh = useRefresh();
  const [mode, setMode] = useState<RemovalMode>("unmanage");
  const name = nameOf(node);
  const remove = useMutation({
    mutationFn: () => nodesApi.remove(node.id, mode),
    onSuccess: async (r) => {
      await refresh(r.node);
      onClose();
    },
  });
  return (
    <ActionDialog
      title={t("nodes.remove.title", { name })}
      onClose={onClose}
      busy={remove.isPending}
      actions={
        <>
          <Button variant="secondary" onClick={onClose} disabled={remove.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button variant="destructive" onClick={() => remove.mutate()} disabled={remove.isPending}>
            {t("nodes.remove.confirm", { name })}
          </Button>
        </>
      }
    >
      <fieldset className="hx-choices">
        <legend className="hx-visually-hidden">{t("nodes.remove.how")}</legend>
        {REMOVAL_MODES.map((m) => {
          const available = m === "unmanage";
          const id = `hx-remove-${m}`;
          return (
            <label key={m} htmlFor={id} className="hx-choice">
              <input
                id={id}
                type="radio"
                name="hx-remove-mode"
                value={m}
                checked={mode === m}
                disabled={!available}
                aria-describedby={`${id}-note`}
                onChange={() => setMode(m)}
              />
              <span className="hx-choice-mark" aria-hidden="true" />
              <span className="hx-choice-text">
                <strong>{t(`nodes.remove.mode.${m}`)}</strong>
                <span id={`${id}-note`} className="hx-sub">
                  {available
                    ? t("nodes.remove.unmanageEffect")
                    : t("nodes.remove.unavailable", {
                        ticket: MODE_TICKET[m as Exclude<RemovalMode, "unmanage">].join(t("nodes.listSeparator")),
                      })}
                </span>
              </span>
            </label>
          );
        })}
      </fieldset>
      <NodeError error={remove.error} />
    </ActionDialog>
  );
}

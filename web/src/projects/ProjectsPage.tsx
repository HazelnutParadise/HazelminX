import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Input } from "@cloudflare/kumo/components/input";
import { Table } from "@cloudflare/kumo/components/table";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { PageHead } from "../shell/PageShell";
import { PageSkeleton, StateBlock } from "../shell/PageStates";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { projectKeys, projectsApi, type Project } from "./api";
import { PROJECTS_WRITE, useProjectPermission } from "./hooks";
import { GuardedButton, ProjectError, When } from "./parts";
import { envHref, projectHref } from "./paths";
import "./projects.css";

/** Names Core accepts for projects and environments (spec 52.1): lowercase letter first, at most 30. */
export const NAME_PATTERN = /^[a-z][a-z0-9-]{0,29}$/;

/** Project list (board Projects「專案與服務」): every project the account can read, with its environments. */
export function ProjectsPage() {
  const { t } = useTranslation();
  useDocumentTitle(t("nav.projects"));
  const canWrite = useProjectPermission(PROJECTS_WRITE);
  const [creating, setCreating] = useState(false);
  const list = useQuery({ queryKey: projectKeys.list, queryFn: ({ signal }) => projectsApi.list(signal) });
  const items = list.data?.items ?? [];
  const add = (
    <GuardedButton
      allowed={canWrite}
      need={t("projects.needWrite")}
      variant="primary"
      onClick={() => setCreating(true)}
    >
      {t("projects.add")}
    </GuardedButton>
  );
  return (
    <>
      <PageHead title={t("nav.projects")} actions={add} />
      {list.isError && !list.data ? (
        <ProjectError error={list.error} />
      ) : !list.data ? (
        <PageSkeleton delayMs={400} />
      ) : items.length === 0 ? (
        <StateBlock
          title={t("projects.empty.title")}
          detail={<p className="hx-note">{t("projects.empty.detail")}</p>}
          actions={add}
        />
      ) : (
        <section className="hx-panel" aria-labelledby="hx-projects-title">
          <div className="hx-panel-head">
            <h2 id="hx-projects-title">{t("projects.listTitle", { count: items.length })}</h2>
          </div>
          <ProjectTable rows={items} />
        </section>
      )}
      {creating && <CreateProjectDialog onClose={() => setCreating(false)} />}
    </>
  );
}

function ProjectTable({ rows }: { rows: Project[] }) {
  const { t } = useTranslation();
  const envs = useQueries({
    queries: rows.map((p) => ({
      queryKey: projectKeys.environments(p.id),
      queryFn: ({ signal }: { signal: AbortSignal }) => projectsApi.environments(p.id, signal),
    })),
  });
  return (
    <div className="hx-table-wrap">
      <Table className="hx-table hx-projects">
        <Table.Header>
          <Table.Row>
            <Table.Head>{t("projects.col.project")}</Table.Head>
            <Table.Head>{t("projects.col.environments")}</Table.Head>
            <Table.Head className="c5">{t("projects.col.description")}</Table.Head>
            <Table.Head className="c5">{t("projects.col.createdAt")}</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.map((p, i) => {
            const e = envs[i];
            return (
              <Table.Row key={p.id}>
                <Table.Cell>
                  <Link to={projectHref(p.id)} className="hx-name hx-row-link hx-mono">
                    {p.name}
                  </Link>
                  {p.displayName && <div className="hx-sub">{p.displayName}</div>}
                </Table.Cell>
                <Table.Cell>
                  {e?.isError ? (
                    <span className="hx-muted">{t("projects.envLoadFailed")}</span>
                  ) : !e?.data ? (
                    <span className="hx-muted">{t("projects.loading")}</span>
                  ) : e.data.items.length === 0 ? (
                    <span className="hx-muted">{t("projects.noEnvironments")}</span>
                  ) : (
                    <ul className="hx-env-chips" aria-label={t("projects.col.environments")}>
                      {e.data.items.map((env) => (
                        <li key={env.id}>
                          <Link to={envHref(p.id, env.id)} className="hx-mono">
                            {env.name}
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </Table.Cell>
                <Table.Cell className="c5">{p.description || <span aria-hidden="true">—</span>}</Table.Cell>
                <Table.Cell className="c5 hx-nowrap">
                  <When iso={p.createdAt} />
                </Table.Cell>
              </Table.Row>
            );
          })}
        </Table.Body>
      </Table>
    </div>
  );
}

/** A dialog with a name field (and optional extra fields) that creates something and closes. */
export function NameDialog({
  title,
  nameLabel,
  extra,
  busy,
  error,
  submitLabel,
  onSubmit,
  onClose,
  dirty,
}: {
  title: string;
  nameLabel: string;
  extra?: React.ReactNode;
  busy: boolean;
  error: unknown;
  submitLabel: string;
  onSubmit: (name: string) => void;
  onClose: () => void;
  dirty: boolean;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const valid = NAME_PATTERN.test(name);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid && !busy) onSubmit(name);
  };
  return (
    <Dialog.Root
      open
      disablePointerDismissal={dirty || name !== "" || busy}
      onOpenChange={(o: boolean) => !o && !busy && onClose()}
    >
      <Dialog className="hx-dialog" size="base">
        <Dialog.Title className="hx-dialog-title">{title}</Dialog.Title>
        <form onSubmit={submit}>
          <div className="hx-dialog-body hx-form">
            <Input
              label={nameLabel}
              value={name}
              spellCheck={false}
              autoComplete="off"
              className="hx-mono"
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
              description={t("projects.nameRule")}
              error={name !== "" && !valid ? t("projects.nameInvalid") : undefined}
            />
            {extra}
            <ProjectError error={error} />
          </div>
          <div className="hx-dialog-actions">
            <Button variant="secondary" onClick={onClose} disabled={busy}>
              {t("projects.cancel")}
            </Button>
            <Button variant="primary" type="submit" disabled={!valid} loading={busy}>
              {submitLabel}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

function CreateProjectDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const create = useMutation({
    mutationFn: (name: string) =>
      projectsApi.create({ name, displayName: displayName.trim(), description: description.trim() }),
    onSuccess: async (r) => {
      await qc.invalidateQueries({ queryKey: projectKeys.list });
      void navigate(projectHref(r.project.id));
    },
  });
  return (
    <NameDialog
      title={t("projects.create.title")}
      nameLabel={t("projects.create.name")}
      submitLabel={t("projects.create.submit")}
      busy={create.isPending}
      error={create.error}
      dirty={displayName !== "" || description !== ""}
      onSubmit={(name) => create.mutate(name)}
      onClose={onClose}
      extra={
        <>
          <Input
            label={t("projects.create.displayName")}
            value={displayName}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDisplayName(e.target.value)}
          />
          <Input
            label={t("projects.create.description")}
            value={description}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDescription(e.target.value)}
          />
        </>
      }
    />
  );
}

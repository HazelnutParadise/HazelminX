import { PlusIcon } from "@phosphor-icons/react";
import { Input } from "@cloudflare/kumo/components/input";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, Navigate, NavLink, useNavigate, useParams } from "react-router";
import { isApiError } from "../auth/api";
import { PageHead } from "../shell/PageShell";
import { PageSkeleton, StateBlock } from "../shell/PageStates";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { projectKeys, projectsApi, type Environment, type Project } from "./api";
import { EnvironmentContent } from "./EnvironmentContent";
import { PROJECTS_WRITE, useProjectPermission } from "./hooks";
import { GuardedButton, ProjectError } from "./parts";
import { NameDialog } from "./ProjectsPage";
import { envHref, PROJECTS_PATH, projectHref } from "./paths";
import "./projects.css";

/**
 * One project (board ProjectDetail): its environments as the tabs of one frame (DESIGN.md「上層的切換要把它管的
 * 內容包在裡面」), and under the selected tab the environment's Compose unit, its preview and its deployments.
 */
export function ProjectPage() {
  const { t } = useTranslation();
  const { id = "", "*": rest = "" } = useParams();
  const [envId = "", sub = ""] = rest.split("/");
  const project = useQuery({ queryKey: projectKeys.one(id), queryFn: () => projectsApi.get(id) });
  const envs = useQuery({
    queryKey: projectKeys.environments(id),
    queryFn: ({ signal }) => projectsApi.environments(id, signal),
  });
  const p = project.data?.project;
  useDocumentTitle(p ? p.displayName || p.name : t("nav.projects"));
  const crumbs = (
    <nav className="hx-crumbs" aria-label={t("projects.breadcrumb")}>
      <Link to={PROJECTS_PATH}>{t("nav.projects")}</Link>
      <span aria-hidden="true"> / </span>
    </nav>
  );
  if (project.isError && !p) {
    const missing = isApiError(project.error, "NOT_FOUND");
    return (
      <>
        {crumbs}
        <StateBlock
          heading
          title={missing ? t("projects.notFound", { id }) : t("projects.loadFailed")}
          detail={missing ? undefined : <ProjectError error={project.error} />}
          actions={
            <Link to={PROJECTS_PATH} className="hx-btn-link">
              {t("projects.backToList")}
            </Link>
          }
        />
      </>
    );
  }
  if (!p || (!envs.data && !envs.isError)) return <PageSkeleton delayMs={400} />;
  const list = envs.data?.items ?? [];
  if (!envId && list.length > 0) return <Navigate to={envHref(p.id, list[0]!.id)} replace />;
  return (
    <>
      {crumbs}
      <PageHead title={p.displayName || p.name} />
      <p className="hx-project-meta">
        <span className="hx-mono">{p.name}</span>
        {p.description && <span>{p.description}</span>}
      </p>
      {envs.isError ? (
        <ProjectError error={envs.error} />
      ) : (
        <EnvironmentFrame project={p} envs={list} envId={envId} sub={sub} />
      )}
    </>
  );
}

function EnvironmentFrame({
  project: p,
  envs,
  envId,
  sub,
}: {
  project: Project;
  envs: Environment[];
  envId: string;
  sub: string;
}) {
  const { t } = useTranslation();
  const canWrite = useProjectPermission(PROJECTS_WRITE, p.id);
  const [adding, setAdding] = useState(false);
  const env = envs.find((e) => e.id === envId);
  return (
    <section className="hx-scope" aria-label={t("projects.env.frame")}>
      <div className="hx-scope-head">
        <span className="hx-scope-label" id="hx-env-label">
          {t("projects.env.label")}
        </span>
        <nav className="hx-scope-tabs" aria-labelledby="hx-env-label">
          {envs.map((e) => (
            <NavLink key={e.id} to={envHref(p.id, e.id)} className="hx-scope-tab hx-mono">
              {e.name}
            </NavLink>
          ))}
        </nav>
        <GuardedButton allowed={canWrite} need={t("projects.needWrite")} onClick={() => setAdding(true)}>
          <PlusIcon size={14} aria-hidden="true" /> {t("projects.env.add")}
        </GuardedButton>
      </div>
      <div className="hx-scope-body">
        {envs.length === 0 ? (
          <StateBlock
            title={t("projects.env.empty.title")}
            detail={<p className="hx-note">{t("projects.env.empty.detail")}</p>}
          />
        ) : !env ? (
          <StateBlock
            title={t("projects.env.notFound")}
            actions={
              <Link to={projectHref(p.id)} className="hx-btn-link">
                {t("projects.env.backToFirst")}
              </Link>
            }
          />
        ) : (
          <EnvironmentContent key={env.id} project={p} env={env} sub={sub} />
        )}
      </div>
      {adding && <CreateEnvironmentDialog project={p} onClose={() => setAdding(false)} />}
    </section>
  );
}

function CreateEnvironmentDialog({ project: p, onClose }: { project: Project; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [description, setDescription] = useState("");
  const create = useMutation({
    mutationFn: (name: string) => projectsApi.createEnvironment(p.id, { name, description: description.trim() }),
    onSuccess: async (r) => {
      await qc.invalidateQueries({ queryKey: projectKeys.environments(p.id) });
      onClose();
      void navigate(envHref(p.id, r.environment.id));
    },
  });
  return (
    <NameDialog
      title={t("projects.env.create.title", { project: p.name })}
      nameLabel={t("projects.env.create.name")}
      submitLabel={t("projects.env.create.submit")}
      busy={create.isPending}
      error={create.error}
      dirty={description !== ""}
      onSubmit={(name) => create.mutate(name)}
      onClose={onClose}
      extra={
        <Input
          label={t("projects.env.create.description")}
          value={description}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => setDescription(e.target.value)}
        />
      }
    />
  );
}

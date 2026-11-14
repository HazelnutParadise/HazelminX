import { LinkButton } from "@cloudflare/kumo/components/button";
import { Table } from "@cloudflare/kumo/components/table";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { isApiError } from "../auth/api";
import { PageSkeleton } from "../shell/PageStates";
import { SEVERITIES, projectKeys, projectsApi, type Analysis, type Environment, type Project, type Unit } from "./api";
import { DeployPanel, DeploymentsPanel } from "./Deploy";
import { PROJECTS_WRITE, useProjectPermission, useHostName } from "./hooks";
import { Panel, ProjectError, SeverityPill, When } from "./parts";
import { bytes } from "./format";
import { UnitEditor } from "./UnitEditor";
import { editHref } from "./paths";

/** What one environment shows: the Compose editor when it has no unit yet (or on /compose), else the unit. */
export function EnvironmentContent({ project: p, env, sub }: { project: Project; env: Environment; sub: string }) {
  const unit = useQuery({
    queryKey: projectKeys.unit(p.id, env.id),
    queryFn: () => projectsApi.unit(p.id, env.id),
    retry: (n, e) => !isApiError(e, "NOT_FOUND") && n < 2,
  });
  const missing = unit.isError && isApiError(unit.error, "NOT_FOUND");
  if (unit.isError && !missing) return <ProjectError error={unit.error} />;
  if (!unit.data && !missing) return <PageSkeleton delayMs={400} />;
  const data = missing ? undefined : unit.data;
  if (!data || sub === "compose") return <UnitEditor project={p} env={env} current={data?.unit} />;
  return <UnitView project={p} env={env} unit={data.unit} analysis={data.analysis} />;
}

function UnitView({
  project: p,
  env,
  unit: u,
  analysis: a,
}: {
  project: Project;
  env: Environment;
  unit: Unit;
  analysis: Analysis;
}) {
  const { t } = useTranslation();
  const canWrite = useProjectPermission(PROJECTS_WRITE, p.id);
  const hostName = useHostName();
  return (
    <div className="hx-env-stack">
      <div className="hx-env-summary">
        <dl className="hx-env-facts">
          <div>
            <dt>{t("projects.unit.composeProject")}</dt>
            <dd className="hx-mono">{u.composeProject}</dd>
          </div>
          <div>
            <dt>{t("projects.unit.source")}</dt>
            <dd>{t(`projects.unit.sources.${u.source}`, { defaultValue: u.source ?? "—" })}</dd>
          </div>
          <div>
            <dt>{t("projects.unit.target")}</dt>
            <dd className="hx-mono">
              {u.target?.nodeId
                ? `${hostName(u.target.nodeId)} · ${u.target.engineEnvironmentId}`
                : t("projects.unit.noTarget")}
            </dd>
          </div>
          <div>
            <dt>{t("projects.unit.version")}</dt>
            <dd>
              {t("projects.unit.versionValue", { version: u.version })} · <When iso={u.updatedAt} />
            </dd>
          </div>
        </dl>
        {canWrite !== false && (
          <LinkButton href={editHref(p.id, env.id)} variant="secondary">
            {t("projects.unit.edit")}
          </LinkButton>
        )}
      </div>
      <FilesPanel unit={u} />
      <FindingsPanel analysis={a} />
      <ServicesPanel analysis={a} />
      <DeployPanel project={p} env={env} unit={u} />
      <DeploymentsPanel project={p} env={env} />
    </div>
  );
}

function FilesPanel({ unit: u }: { unit: Unit }) {
  const { t } = useTranslation();
  const files = u.files ?? [];
  const extra = u.extraFiles ?? [];
  const role = (path: string | null) =>
    path === u.mainFile
      ? t("projects.files.roleMain")
      : extra.includes(path ?? "")
        ? t("projects.files.roleExtra", { n: extra.indexOf(path ?? "") + 2 })
        : t("projects.files.roleOther");
  return (
    <Panel id="hx-unit-files" title={t("projects.files.title", { count: files.length })}>
      <div className="hx-table-wrap">
        <Table className="hx-table">
          <Table.Header>
            <Table.Row>
              <Table.Head>{t("projects.files.path")}</Table.Head>
              <Table.Head>{t("projects.files.role")}</Table.Head>
              <Table.Head>{t("projects.files.size")}</Table.Head>
              <Table.Head className="c5">{t("projects.files.sha256")}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {files.map((f) => (
              <Table.Row key={f.path}>
                <Table.Cell className="hx-mono">
                  {f.path}
                  {f.secret && <span className="hx-tag-secret">{t("projects.files.secret")}</span>}
                </Table.Cell>
                <Table.Cell>{role(f.path)}</Table.Cell>
                <Table.Cell className="hx-nowrap">{bytes(f.size)}</Table.Cell>
                <Table.Cell className="c5 hx-mono hx-hash" title={f.sha256 ?? undefined}>
                  {(f.sha256 ?? "").slice(0, 12)}
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </div>
      {(u.profiles ?? []).length > 0 || u.workingDir ? (
        <p className="hx-note hx-panel-note">
          {t("projects.files.options", {
            profiles: (u.profiles ?? []).join(", ") || "—",
            workingDir: u.workingDir || ".",
          })}
        </p>
      ) : null}
    </Panel>
  );
}

/** Validation findings (accepted, refused, kept but not applied) and the variables the files use. */
export function FindingsPanel({ analysis: a, title }: { analysis: Analysis; title?: string }) {
  const { t } = useTranslation();
  const findings = [...(a.findings ?? [])].sort(
    (x, y) => SEVERITIES.indexOf(x.severity as never) - SEVERITIES.indexOf(y.severity as never),
  );
  const counts = SEVERITIES.map((s) => ({ s, n: findings.filter((f) => f.severity === s).length })).filter(
    (c) => c.n > 0,
  );
  const vars = a.variables ?? [];
  return (
    <Panel
      id="hx-unit-findings"
      title={title ?? t("projects.findings.title")}
      extra={
        <span className="hx-note">
          {a.valid ? t("projects.findings.valid") : t("projects.findings.invalid")}
          {counts.map((c) => ` · ${t(`projects.severity.${c.s}`)} ${c.n}`).join("")}
        </span>
      }
    >
      {findings.length === 0 ? (
        <p className="hx-note hx-panel-note">{t("projects.findings.none")}</p>
      ) : (
        <div className="hx-table-wrap">
          <Table className="hx-table hx-findings">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("projects.findings.result")}</Table.Head>
                <Table.Head>{t("projects.findings.where")}</Table.Head>
                <Table.Head>{t("projects.findings.message")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {findings.map((f, i) => (
                <Table.Row key={`${f.code}-${f.attribute}-${i}`}>
                  <Table.Cell>
                    <SeverityPill value={f.severity} />
                  </Table.Cell>
                  <Table.Cell className="hx-mono">
                    {f.file || "—"}
                    {f.attribute && <div className="hx-sub">{f.attribute}</div>}
                  </Table.Cell>
                  <Table.Cell>
                    <span className="hx-mono hx-code">{f.code}</span>
                    <div className="hx-raw">{f.message}</div>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
      {vars.length > 0 && (
        <div className="hx-table-wrap hx-subtable">
          <Table className="hx-table">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("projects.vars.name")}</Table.Head>
                <Table.Head>{t("projects.vars.state")}</Table.Head>
                <Table.Head>{t("projects.vars.default")}</Table.Head>
                <Table.Head className="c5">{t("projects.vars.files")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {vars.map((v) => (
                <Table.Row key={v.name}>
                  <Table.Cell className="hx-mono">{v.name}</Table.Cell>
                  <Table.Cell>{t(`projects.vars.states.${v.state}`, { defaultValue: v.state ?? "—" })}</Table.Cell>
                  <Table.Cell className="hx-mono">{v.default || "—"}</Table.Cell>
                  <Table.Cell className="c5 hx-mono">{(v.files ?? []).join(", ") || "—"}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
    </Panel>
  );
}

/** The services Docker Compose reads from the files (graphical view of the unit; editing is in the raw editor). */
export function ServicesPanel({ analysis: a }: { analysis: Analysis }) {
  const { t } = useTranslation();
  const services = a.services ?? [];
  if (services.length === 0) return null;
  return (
    <Panel id="hx-unit-services" title={t("projects.services.title", { count: services.length })}>
      <div className="hx-table-wrap">
        <Table className="hx-table hx-services">
          <Table.Header>
            <Table.Row>
              <Table.Head>{t("projects.services.name")}</Table.Head>
              <Table.Head>{t("projects.services.image")}</Table.Head>
              <Table.Head>{t("projects.services.ports")}</Table.Head>
              <Table.Head className="c5">{t("projects.services.mounts")}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {services.map((s) => (
              <Table.Row key={s.name}>
                <Table.Cell className="hx-mono">
                  {s.name}
                  {!s.enabled && (
                    <div className="hx-sub">
                      {t("projects.services.disabled", { profiles: (s.profiles ?? []).join(", ") })}
                    </div>
                  )}
                </Table.Cell>
                <Table.Cell className="hx-mono">{s.image || (s.build ? t("projects.services.build") : "—")}</Table.Cell>
                <Table.Cell className="hx-mono">
                  {(s.ports ?? []).length === 0
                    ? "—"
                    : (s.ports ?? []).map((pt) => (
                        <div key={`${pt.hostIp}-${pt.published}-${pt.target}-${pt.protocol}`}>
                          {pt.published ? `${pt.hostIp ? pt.hostIp + ":" : ""}${pt.published} → ` : ""}
                          {pt.target}/{pt.protocol}
                        </div>
                      ))}
                </Table.Cell>
                <Table.Cell className="c5 hx-mono">
                  {(s.mounts ?? []).length === 0
                    ? "—"
                    : (s.mounts ?? []).map((m) => (
                        <div key={`${m.source}-${m.target}`}>
                          {m.source ? `${m.source} → ` : ""}
                          {m.target}
                          {m.readOnly ? " (ro)" : ""}
                        </div>
                      ))}
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </div>
    </Panel>
  );
}

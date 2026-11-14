import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Input } from "@cloudflare/kumo/components/input";
import { Table } from "@cloudflare/kumo/components/table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { isApiError } from "../auth/api";
import { Notice } from "../auth/parts";
import {
  JOB_ENDED,
  jobsApi,
  PLAN,
  plansApi,
  projectKeys,
  projectsApi,
  requestId,
  TARGET_READY,
  type Deployment,
  type Environment,
  type Job,
  type Plan,
  type Project,
  type Unit,
} from "./api";
import { PROJECTS_DEPLOY, useIsOwner, useProjectPermission, useHostName } from "./hooks";
import {
  CheckPill,
  DeploymentPill,
  GuardedButton,
  JobPill,
  Panel,
  PlanPill,
  ProjectError,
  SeverityPill,
  StepPill,
  When,
} from "./parts";

const PLAN_PARAM = "plan";
const CHECK_PARAM = "check";

/**
 * The deployment preview and the path to a deployment (boards DeployCheck, DeployConfirm): what Core knows
 * before touching the host, the host check job, then the change plan: create, approve, run, follow the job.
 * The plan and the host check job are kept in the URL so a reload comes back to them.
 */
export function DeployPanel({ project: p, env, unit: u }: { project: Project; env: Environment; unit: Unit }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const planId = params.get(PLAN_PARAM) ?? "";
  const checkJob = params.get(CHECK_PARAM) ?? "";
  const canDeploy = useProjectPermission(PROJECTS_DEPLOY, p.id);
  const hostName = useHostName();
  const preview = useQuery({
    queryKey: projectKeys.preview(p.id, env.id),
    queryFn: () => projectsApi.preview(p.id, env.id),
    refetchInterval: 15_000,
  });
  const setParam = (key: string, value: string) =>
    setParams(
      (sp) => {
        const next = new URLSearchParams(sp);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: false },
    );
  // One request id per host check until Core answers, so a retry after a lost reply gets the same job.
  const checkRid = useRef("");
  const hostCheck = useMutation({
    mutationFn: () => {
      checkRid.current ||= requestId();
      return projectsApi.hostCheck(p.id, env.id, checkRid.current);
    },
    onSuccess: (r) => {
      checkRid.current = "";
      setParam(CHECK_PARAM, r.jobId);
    },
  });
  const createPlan = useMutation({
    mutationFn: () => plansApi.createDeploy(p.id, env.id),
    onSuccess: (r) => {
      qc.setQueryData(projectKeys.plan(r.plan.id ?? ""), r);
      setParam(PLAN_PARAM, r.plan.id ?? "");
    },
  });
  const pv = preview.data?.preview;
  const target = pv?.target;
  const blockers = pv?.blockers ?? [];
  const pending = pv?.hostChecksPending ?? [];
  const need = t("projects.needDeploy");
  return (
    <>
      <Panel
        id="hx-deploy-preview"
        title={t("projects.preview.title")}
        extra={
          <>
            <GuardedButton
              allowed={canDeploy}
              need={need}
              disabled={!u.target?.nodeId}
              loading={hostCheck.isPending}
              onClick={() => hostCheck.mutate()}
            >
              {t("projects.preview.hostCheck")}
            </GuardedButton>
            <GuardedButton
              allowed={canDeploy}
              need={need}
              variant="primary"
              disabled={!pv?.deployable}
              loading={createPlan.isPending}
              onClick={() => createPlan.mutate()}
            >
              {t("projects.preview.createPlan")}
            </GuardedButton>
          </>
        }
      >
        <div className="hx-panel-body">
          {preview.isError && !pv ? (
            <ProjectError error={preview.error} />
          ) : !pv ? (
            <p className="hx-note">{t("projects.loading")}</p>
          ) : (
            <>
              <dl className="hx-env-facts">
                <div>
                  <dt>{t("projects.preview.target")}</dt>
                  <dd>
                    <span className={target?.state === TARGET_READY ? "hx-ok-text" : "hx-danger-text"}>
                      {t(`projects.targetState.${target?.state}`, { defaultValue: target?.state ?? "—" })}
                    </span>
                    {target?.nodeId && (
                      <div className="hx-sub hx-mono">
                        {hostName(target.nodeId)} · {target.engineEnvironmentId}
                      </div>
                    )}
                  </dd>
                </div>
                <div>
                  <dt>{t("projects.preview.engine")}</dt>
                  <dd className="hx-mono">
                    {[target?.kind, target?.engineVersion, target?.mode].filter(Boolean).join(" · ") || "—"}
                    {target?.composeVersion && <div className="hx-sub">Compose {target.composeVersion}</div>}
                  </dd>
                </div>
                <div>
                  <dt>{t("projects.preview.deployable")}</dt>
                  <dd>{pv.deployable ? t("projects.preview.yes") : t("projects.preview.no")}</dd>
                </div>
                <div>
                  <dt>{t("projects.preview.approval")}</dt>
                  <dd>{pv.needsApproval ? t("projects.preview.needsApproval") : t("projects.preview.noApproval")}</dd>
                </div>
              </dl>
              {blockers.length > 0 && (
                <ul className="hx-check-list" aria-label={t("projects.preview.blockers")}>
                  {blockers.map((b, i) => (
                    <li key={`${b.code}-${i}`}>
                      <SeverityPill value="error" />
                      <span className="hx-mono hx-code">{b.code}</span>
                      <span className="hx-raw">{b.message}</span>
                    </li>
                  ))}
                </ul>
              )}
              {pending.length > 0 && (
                <p className="hx-note">
                  {t("projects.preview.pending", {
                    checks: pending
                      .map((c) => t(`projects.hostCheck.${c}`, { defaultValue: c }))
                      .join(t("projects.sep")),
                  })}
                </p>
              )}
            </>
          )}
          <ProjectError error={hostCheck.error ?? createPlan.error} />
        </div>
      </Panel>
      {checkJob && (
        <JobPanel
          id="hx-host-check"
          title={t("projects.hostCheckJob.title")}
          jobId={checkJob}
          onClose={() => setParam(CHECK_PARAM, "")}
        />
      )}
      {planId && (
        <PlanPanel
          key={planId}
          planId={planId}
          project={p}
          env={env}
          canDeploy={canDeploy}
          onClose={() => setParam(PLAN_PARAM, "")}
          onNewPlan={() => createPlan.mutate()}
        />
      )}
    </>
  );
}

function PlanPanel({
  planId,
  project: p,
  env,
  canDeploy,
  onClose,
  onNewPlan,
}: {
  planId: string;
  project: Project;
  env: Environment;
  canDeploy: boolean | undefined;
  onClose: () => void;
  onNewPlan: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const isOwner = useIsOwner();
  const [approving, setApproving] = useState(false);
  const plan = useQuery({
    queryKey: projectKeys.plan(planId),
    queryFn: () => plansApi.get(planId),
    refetchInterval: (q) => {
      const s = q.state.data?.plan.state;
      return s === PLAN.awaiting || s === PLAN.approved ? 5_000 : false;
    },
  });
  const refresh = async (r?: { plan: Plan }) => {
    if (r) qc.setQueryData(projectKeys.plan(planId), r);
    await qc.invalidateQueries({ queryKey: projectKeys.deployments(p.id, env.id) });
  };
  // Core answers the same request id with the same job: keep one id for this plan so a retry after a lost
  // reply does not become REQUEST_ID_CONFLICT. A conflict still means the plan already runs: read it again.
  const execRid = useRef("");
  const execute = useMutation({
    mutationFn: async (pl: Plan) => {
      execRid.current ||= requestId();
      try {
        return await plansApi.execute(pl.id ?? "", pl.digest ?? "", execRid.current);
      } catch (e) {
        if (isApiError(e, "REQUEST_ID_CONFLICT")) {
          await qc.invalidateQueries({ queryKey: projectKeys.plan(planId) });
          return undefined;
        }
        throw e;
      }
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: projectKeys.plan(planId) });
      await refresh();
    },
  });
  const reject = useMutation({
    mutationFn: () => plansApi.withdraw(planId, t("projects.plan.withdrawReason")),
    onSuccess: (r) => refresh(r),
  });
  const pl = plan.data?.plan;
  if (!pl) {
    return (
      <Panel id="hx-plan" title={t("projects.plan.title")}>
        <div className="hx-panel-body">
          {plan.isError ? <ProjectError error={plan.error} /> : <p className="hx-note">{t("projects.loading")}</p>}
        </div>
      </Panel>
    );
  }
  const checks = pl.checks ?? [];
  const high = checks.filter((c) => c.status === "high_risk" || c.status === "warning");
  const ownerBlocked = pl.approval?.ownerRequired && isOwner === false;
  const jobId = pl.execution?.jobId ?? "";
  const target = (pl.targets ?? [])[0];
  return (
    <>
      <Panel
        id="hx-plan"
        title={
          <>
            {t("projects.plan.title")} <PlanPill value={pl.state} />
          </>
        }
        extra={
          <>
            {pl.state === PLAN.awaiting && (
              <>
                <GuardedButton
                  allowed={canDeploy}
                  need={t("projects.needDeploy")}
                  loading={reject.isPending}
                  onClick={() => reject.mutate()}
                >
                  {t("projects.plan.withdraw")}
                </GuardedButton>
                <GuardedButton
                  allowed={ownerBlocked ? false : canDeploy}
                  need={ownerBlocked ? t("projects.plan.ownerRequired") : t("projects.needDeploy")}
                  variant="primary"
                  onClick={() => setApproving(true)}
                >
                  {t("projects.plan.approve")}
                </GuardedButton>
              </>
            )}
            {pl.state === PLAN.approved && (
              <GuardedButton
                allowed={canDeploy}
                need={t("projects.needDeploy")}
                variant="primary"
                disabled={!pl.executable}
                loading={execute.isPending}
                onClick={() => execute.mutate(pl)}
              >
                {t("projects.plan.execute", { host: target?.nodeName || target?.node || "" })}
              </GuardedButton>
            )}
            {(pl.state === PLAN.invalidated ||
              pl.state === PLAN.expired ||
              pl.state === PLAN.rejected ||
              pl.state === PLAN.withdrawn) && (
              <GuardedButton allowed={canDeploy} need={t("projects.needDeploy")} onClick={onNewPlan}>
                {t("projects.plan.newPlan")}
              </GuardedButton>
            )}
            <Button variant="secondary" onClick={onClose}>
              {t("projects.close")}
            </Button>
          </>
        }
      >
        <div className="hx-panel-body hx-plan-body">
          <p className="hx-mono hx-plan-summary">{pl.summary}</p>
          <dl className="hx-env-facts">
            <div>
              <dt>{t("projects.plan.target")}</dt>
              <dd className="hx-mono">
                {(pl.targets ?? []).map((x) => (
                  <div key={x.key}>
                    {x.name} · {x.nodeName || x.node || "—"} · {x.environment}
                    <div className="hx-sub">{t("projects.unit.versionValue", { version: x.version })}</div>
                  </div>
                ))}
              </dd>
            </div>
            <div>
              <dt>{t("projects.plan.risk")}</dt>
              <dd>{t(`projects.risk.${pl.risk}`, { defaultValue: pl.risk ?? "—" })}</dd>
            </div>
            <div>
              <dt>{t("projects.plan.approval")}</dt>
              <dd>
                {pl.approval?.autoApproved
                  ? t("projects.plan.autoApproved")
                  : pl.approval?.approvedBy
                    ? t("projects.plan.approvedBy", { name: pl.approval.approvedBy.name })
                    : pl.approval?.ownerRequired
                      ? t("projects.plan.needsOwner")
                      : pl.approval?.required
                        ? t("projects.plan.needsApproval")
                        : "—"}
              </dd>
            </div>
            <div>
              <dt>{t("projects.plan.expires")}</dt>
              <dd>
                <When iso={pl.expiresAt} />
              </dd>
            </div>
          </dl>
          {pl.invalidation && (
            <Notice tone="danger">
              {t("projects.plan.invalidated")}
              <ul className="hx-plain-list">
                {(pl.invalidation.targets ?? []).map((m) => (
                  <li key={m.key} className="hx-mono">
                    {t("projects.plan.moved", { name: m.name, from: m.plannedVersion, to: m.currentVersion })}
                  </li>
                ))}
              </ul>
            </Notice>
          )}
          {pl.decision && (
            <p className="hx-note">
              {t("projects.plan.decision", { name: pl.decision.by?.name ?? "—", reason: pl.decision.reason })}
            </p>
          )}
          <h3 className="hx-h3">{t("projects.plan.checks", { count: checks.length })}</h3>
          <ul className="hx-check-list">
            {checks.map((c) => (
              <li key={c.id}>
                <CheckPill value={c.status} />
                <span className="hx-mono hx-code">{c.title}</span>
                <span className="hx-raw">{c.detail}</span>
                {(c.evidence ?? []).length > 0 && (
                  <span className="hx-sub hx-mono">{(c.evidence ?? []).join(" · ")}</span>
                )}
              </li>
            ))}
          </ul>
          <h3 className="hx-h3">{t("projects.plan.affected", { count: (pl.affected ?? []).length })}</h3>
          <ul className="hx-plain-list hx-mono">
            {(pl.affected ?? []).map((a) => (
              <li key={a.id}>{`${a.name} · ${a.effect}`}</li>
            ))}
          </ul>
          <h3 className="hx-h3">{t("projects.plan.changes", { count: (pl.changes ?? []).length })}</h3>
          <ul className="hx-plain-list hx-mono">
            {(pl.changes ?? []).map((c, i) => (
              <li key={`${c.path}-${i}`}>
                {c.op} {c.path}
                {c.secret ? ` (${t("projects.files.secret")})` : ""}
              </li>
            ))}
          </ul>
          <ProjectError error={execute.error ?? reject.error} />
        </div>
      </Panel>
      {jobId && <JobPanel id="hx-deploy-job" title={t("projects.deployJob.title")} jobId={jobId} deploy />}
      {approving && (
        <ApproveDialog
          plan={pl}
          env={env}
          high={high}
          onClose={() => setApproving(false)}
          onDone={async (r) => {
            await refresh(r);
            setApproving(false);
          }}
        />
      )}
    </>
  );
}

/**
 * Approval (board DeployConfirm): names the real target, lists every high-risk check and warning, and asks for
 * the environment name typed in, so an approval is never a bare「確定嗎」(UI-006).
 */
function ApproveDialog({
  plan: pl,
  env,
  high,
  onClose,
  onDone,
}: {
  plan: Plan;
  env: Environment;
  high: NonNullable<Plan["checks"]>;
  onClose: () => void;
  onDone: (r: { plan: Plan }) => void;
}) {
  const { t } = useTranslation();
  const [typed, setTyped] = useState("");
  const approve = useMutation({ mutationFn: () => plansApi.approve(pl.id ?? "", pl.digest ?? ""), onSuccess: onDone });
  const target = (pl.targets ?? [])[0];
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (typed === env.name && !approve.isPending) approve.mutate();
  };
  return (
    <Dialog.Root open disablePointerDismissal onOpenChange={(o: boolean) => !o && !approve.isPending && onClose()}>
      <Dialog className="hx-dialog" size="base">
        <Dialog.Title className="hx-dialog-title">
          {t("projects.approve.title", { name: target?.name ?? "", host: target?.nodeName || target?.node || "—" })}
        </Dialog.Title>
        <form onSubmit={submit}>
          <div className="hx-dialog-body hx-form">
            <p>{t("projects.approve.what", { env: env.name, host: target?.nodeName || target?.node || "—" })}</p>
            {high.length > 0 && (
              <ul className="hx-check-list">
                {high.map((c) => (
                  <li key={c.id}>
                    <CheckPill value={c.status} />
                    <span className="hx-mono hx-code">{c.title}</span>
                    <span className="hx-raw">{c.detail}</span>
                  </li>
                ))}
              </ul>
            )}
            <Notice tone="warn">{t("projects.approve.noRollback")}</Notice>
            <Input
              label={t("projects.approve.typeEnv", { env: env.name })}
              value={typed}
              autoComplete="off"
              spellCheck={false}
              className="hx-mono"
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setTyped(e.target.value)}
            />
            <ProjectError error={approve.error} />
          </div>
          <div className="hx-dialog-actions">
            <Button variant="secondary" onClick={onClose} disabled={approve.isPending}>
              {t("projects.cancel")}
            </Button>
            <Button variant="primary" type="submit" disabled={typed !== env.name} loading={approve.isPending}>
              {t("projects.approve.submit", { env: env.name })}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  );
}

/** A job's progress: state, each step with its log tail and error, and the completion checks. */
function JobPanel({
  id,
  title,
  jobId,
  deploy = false,
  onClose,
}: {
  id: string;
  title: string;
  jobId: string;
  deploy?: boolean;
  onClose?: () => void;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const job = useQuery({
    queryKey: projectKeys.job(jobId),
    queryFn: async () => {
      const r = await jobsApi.get(jobId);
      if (JOB_ENDED.includes(r.job.state ?? "")) await qc.invalidateQueries({ queryKey: ["projects", "deployments"] });
      return r;
    },
    refetchInterval: (q) => (JOB_ENDED.includes(q.state.data?.job.state ?? "") ? false : 2_000),
  });
  const j: Job | undefined = job.data?.job;
  return (
    <Panel
      id={id}
      title={
        <>
          {title} {j && <JobPill value={j.state} />}
        </>
      }
      extra={
        onClose && (
          <Button variant="secondary" onClick={onClose}>
            {t("projects.close")}
          </Button>
        )
      }
    >
      <div className="hx-panel-body">
        {!j ? (
          job.isError ? (
            <ProjectError error={job.error} />
          ) : (
            <p className="hx-note">{t("projects.loading")}</p>
          )
        ) : (
          <>
            <p className="hx-note">
              <span className="hx-mono">{j.id}</span> · {t("projects.job.progress", { n: j.progress ?? 0 })}
              {j.stateReason && <span className="hx-raw"> · {j.stateReason}</span>}
            </p>
            <ol className="hx-steps">
              {(j.steps ?? []).map((s) => (
                <li key={s.index}>
                  <div className="hx-step-head">
                    <StepPill value={s.status} />
                    <span>{s.name}</span>
                  </div>
                  {s.error && (
                    <div className="hx-step-error">
                      <span className="hx-mono hx-code">{s.error.code}</span>
                      <span className="hx-raw">{s.error.summary}</span>
                      {(s.error.evidence ?? []).length > 0 && (
                        <pre className="hx-log" tabIndex={0}>
                          {(s.error.evidence ?? []).join("\n")}
                        </pre>
                      )}
                      {s.error.nothingChanged && <span className="hx-note">{t("projects.job.nothingChanged")}</span>}
                    </div>
                  )}
                  {(s.logTail ?? []).length > 0 && (
                    <pre className="hx-log" tabIndex={0}>
                      {(s.logTail ?? []).join("\n")}
                    </pre>
                  )}
                </li>
              ))}
            </ol>
            {deploy &&
              (j.state === "failed" || j.state === "needs_attention" || j.state === "cancelled") &&
              !(j.steps ?? []).some((s) => s.error?.nothingChanged) && (
                <Notice tone="danger">{t("projects.job.failedNoRollback")}</Notice>
              )}
            {(j.completion?.checks ?? []).length > 0 && (
              <ul className="hx-check-list">
                {(j.completion?.checks ?? []).map((c) => (
                  <li key={c.name}>
                    <CheckPill value={c.passed ? "pass" : c.required ? "failed" : "warning"} />
                    <span className="hx-mono hx-code">{c.name}</span>
                    <span className="hx-raw">{c.detail}</span>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    </Panel>
  );
}

interface ContainerState {
  name: string;
  state: string;
}

function containersOf(d: Deployment): ContainerState[] {
  const r = d.result as { containers?: ContainerState[] } | null | undefined;
  return Array.isArray(r?.containers) ? r.containers : [];
}

/** The unit's deployments, newest first; a failed one says what it may have left on the host. */
export function DeploymentsPanel({ project: p, env }: { project: Project; env: Environment }) {
  const { t } = useTranslation();
  const [, setParams] = useSearchParams();
  const hostName = useHostName();
  const list = useQuery({
    queryKey: projectKeys.deployments(p.id, env.id),
    queryFn: () => projectsApi.deployments(p.id, env.id),
    refetchInterval: (q) => ((q.state.data?.deployments ?? []).some((d) => d.state === "running") ? 3_000 : 30_000),
  });
  const rows = list.data?.deployments ?? [];
  return (
    <Panel id="hx-deployments" title={t("projects.deployments.title", { count: rows.length })}>
      {list.isError && !list.data ? (
        <div className="hx-panel-body">
          <ProjectError error={list.error} />
        </div>
      ) : rows.length === 0 ? (
        <p className="hx-note hx-panel-note">{list.data ? t("projects.deployments.none") : t("projects.loading")}</p>
      ) : (
        <div className="hx-table-wrap">
          <Table className="hx-table hx-deployments hx-stack-sm">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("projects.deployments.deployment")}</Table.Head>
                <Table.Head>{t("projects.deployments.state")}</Table.Head>
                <Table.Head>{t("projects.deployments.started")}</Table.Head>
                <Table.Head>{t("projects.deployments.result")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.map((d) => {
                const cs = containersOf(d);
                return (
                  <Table.Row key={d.id}>
                    <Table.Cell className="hx-mono">
                      {d.revision}
                      <div className="hx-sub">
                        {hostName(d.nodeId)} · {t("projects.unit.versionValue", { version: d.unitVersion })}
                      </div>
                      {d.planId && (
                        <button
                          type="button"
                          className="hx-link-btn"
                          onClick={() =>
                            setParams((sp) => {
                              const n = new URLSearchParams(sp);
                              n.set(PLAN_PARAM, d.planId ?? "");
                              return n;
                            })
                          }
                        >
                          {t("projects.deployments.openPlan")}
                        </button>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <DeploymentPill value={d.state} />
                    </Table.Cell>
                    <Table.Cell className="hx-nowrap">
                      <When iso={d.startedAt} />
                      {d.finishedAt && (
                        <div className="hx-sub">
                          <When iso={d.finishedAt} />
                        </div>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      {d.state === "failed" && (
                        <>
                          <span className="hx-mono hx-code">{d.errorCode}</span>
                          <div className="hx-raw">{d.errorMessage}</div>
                          <div className="hx-danger-text">{t("projects.deployments.leftBehind")}</div>
                        </>
                      )}
                      {cs.length > 0 && (
                        <ul className="hx-plain-list hx-mono">
                          {cs.map((c) => (
                            <li key={c.name}>
                              {c.name} · {c.state}
                            </li>
                          ))}
                        </ul>
                      )}
                      {d.state === "running" && <span className="hx-note">{t("projects.deployments.running")}</span>}
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </div>
      )}
    </Panel>
  );
}

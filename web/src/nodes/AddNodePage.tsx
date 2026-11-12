import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { CheckIcon, CopyIcon } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router";
import { isApiError } from "../auth/api";
import { Notice, StepBar, type StepState } from "../auth/parts";
import { StatusPill } from "../components/StatusPill";
import { parseRfc3339 } from "../lib/time";
import { PageHead } from "../shell/PageShell";
import { StateBlock } from "../shell/PageStates";
import { useDocumentTitle } from "../shell/useDocumentTitle";
import { useNow } from "../shell/useNow";
import { CODE_TTL, DEFAULT_AGENT_PORT, ENROLLMENT, nodeKeys, nodesApi, type Enrollment } from "./api";
import { useCanWriteNodes } from "./hooks";
import { decide, enrollCommand, guessAgentUrl, isAgentUrl } from "./enroll";
import { nodeHref, NODES_PATH } from "./paths";
import { Field, NodeError, WriteButton } from "./parts";
import { at, parseTags } from "./status";
import "./nodes.css";

/** Validity choices offered, in seconds (Core accepts 1 to 60 minutes). */
const TTL_CHOICES = [300, 600, 1800, 3600];
/** How often the waiting screen asks Core whether the host used the code. */
const POLL_MS = 3_000;

interface Settings {
  name: string;
  tags: string;
  group: string;
  ttl: number;
  coreUrl: string;
}

/**
 * Add a host (board AddNode): settings → one-time code and the command to run on the host → check what the
 * host reported and approve or reject → done. The code lives only in this component: after a reload the
 * page shows the enrollment by its id without the code.
 */
export function AddNodePage() {
  const { t } = useTranslation();
  useDocumentTitle(t("nodes.add"));
  const [params, setParams] = useSearchParams();
  const enrollmentId = params.get("enrollment");
  const canWrite = useCanWriteNodes();
  const [settings, setSettings] = useState<Settings>({
    name: "",
    tags: "",
    group: "",
    ttl: CODE_TTL.default,
    coreUrl: guessAgentUrl(window.location.hostname, DEFAULT_AGENT_PORT),
  });
  const [code, setCode] = useState<string | null>(null);
  const client = useQueryClient();

  const create = useMutation({
    mutationFn: () =>
      nodesApi.createEnrollment({
        name: settings.name.trim(),
        tags: parseTags(settings.tags),
        group: settings.group.trim(),
        ttlSeconds: settings.ttl,
      }),
    onSuccess: (r) => {
      setCode(r.code);
      client.setQueryData(nodeKeys.enrollment(r.enrollment.id), { enrollment: r.enrollment });
      setParams({ enrollment: r.enrollment.id }, { replace: false });
    },
  });

  const restart = () => {
    setCode(null);
    create.reset();
    setParams({}, { replace: false });
  };

  return (
    <>
      <nav className="hx-crumbs" aria-label={t("nodes.breadcrumb")}>
        <Link to={NODES_PATH}>{t("nodes.title")}</Link>
        <span aria-hidden="true"> / </span>
      </nav>
      <PageHead
        title={t("nodes.add")}
        actions={
          <Link to={NODES_PATH} className="hx-btn-link">
            {t("nodes.addFlow.leave")}
          </Link>
        }
      />
      {enrollmentId ? (
        <EnrollmentFlow id={enrollmentId} code={code} settings={settings} onRestart={restart} canWrite={canWrite} />
      ) : (
        <>
          <StepBar steps={steps("settings", t)} />
          <SettingsForm
            settings={settings}
            onChange={setSettings}
            busy={create.isPending}
            canWrite={canWrite}
            error={create.error}
            onSubmit={() => create.mutate()}
          />
        </>
      )}
    </>
  );
}

type Step = "settings" | "run" | "check" | "done";
const STEPS: Step[] = ["settings", "run", "check", "done"];

function steps(current: Step, t: (k: string) => string): { label: string; state: StepState }[] {
  const i = STEPS.indexOf(current);
  return STEPS.map((s, j) => ({
    label: t(`nodes.addFlow.step.${s}`),
    state: j < i || current === "done" ? "done" : j === i ? "current" : "todo",
  }));
}

function SettingsForm({
  settings,
  onChange,
  onSubmit,
  busy,
  canWrite,
  error,
}: {
  settings: Settings;
  onChange: (s: Settings) => void;
  onSubmit: () => void;
  busy: boolean;
  canWrite: boolean | undefined;
  error: unknown;
}) {
  const { t } = useTranslation();
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => onChange({ ...settings, [k]: v });
  const minutes = (s: number) => t("nodes.addFlow.minutes", { count: s / 60 });
  return (
    <section className="hx-panel" aria-labelledby="hx-add-settings">
      <div className="hx-panel-head">
        <h2 id="hx-add-settings">{t("nodes.addFlow.settingsTitle")}</h2>
      </div>
      <form
        className="hx-panel-body hx-form hx-form--grid"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
      >
        <Input
          label={t("nodes.fields.name")}
          description={t("nodes.addFlow.nameHint")}
          value={settings.name}
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => set("name", e.target.value)}
        />
        <Input
          label={t("nodes.fields.tags")}
          description={t("nodes.fields.tagsHint")}
          value={settings.tags}
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => set("tags", e.target.value)}
        />
        <Input
          label={t("nodes.fields.group")}
          description={t("nodes.addFlow.groupHint")}
          value={settings.group}
          spellCheck={false}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => set("group", e.target.value)}
        />
        <Select
          label={t("nodes.addFlow.ttl")}
          items={TTL_CHOICES.map((v) => ({ value: v, label: minutes(v) }))}
          value={settings.ttl}
          onValueChange={(v: unknown) => typeof v === "number" && set("ttl", v)}
        />
        <Input
          className="hx-form-wide"
          label={t("nodes.addFlow.coreUrl")}
          description={t("nodes.addFlow.coreUrlHint", { port: DEFAULT_AGENT_PORT })}
          value={settings.coreUrl}
          spellCheck={false}
          inputMode="url"
          onChange={(e: React.ChangeEvent<HTMLInputElement>) => set("coreUrl", e.target.value)}
          error={
            settings.coreUrl.trim() && !isAgentUrl(settings.coreUrl) ? t("nodes.addFlow.coreUrlInvalid") : undefined
          }
        />
        <div className="hx-form-wide">
          <NodeError error={error} />
        </div>
        <div className="hx-form-actions hx-form-wide">
          <WriteButton
            allowed={canWrite}
            type="submit"
            variant="primary"
            disabled={busy || !isAgentUrl(settings.coreUrl)}
          >
            {t("nodes.addFlow.create")}
          </WriteButton>
        </div>
      </form>
    </section>
  );
}

function EnrollmentFlow({
  id,
  code,
  settings,
  onRestart,
  canWrite,
}: {
  id: string;
  code: string | null;
  settings: Settings;
  onRestart: () => void;
  canWrite: boolean | undefined;
}) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: nodeKeys.enrollment(id),
    queryFn: () => nodesApi.enrollment(id),
    refetchInterval: (query) => {
      const s = query.state.data?.enrollment.state;
      return s === ENROLLMENT.waiting || s === ENROLLMENT.pending ? POLL_MS : false;
    },
  });
  if (q.isError && !q.data) {
    // Only a missing enrollment means starting over; any other failure keeps the id so the page can try again.
    const missing = isApiError(q.error, "NOT_FOUND");
    return (
      <StateBlock
        title={missing ? t("nodes.addFlow.notFound") : t("nodes.addFlow.loadFailed")}
        detail={missing ? undefined : <NodeError error={q.error} />}
        actions={
          missing ? (
            <Button variant="primary" onClick={onRestart}>
              {t("nodes.addFlow.again")}
            </Button>
          ) : (
            <Button variant="primary" onClick={() => void q.refetch()} disabled={q.isFetching}>
              {t("nodes.addFlow.retry")}
            </Button>
          )
        }
      />
    );
  }
  if (!q.data) return null;
  const e = q.data.enrollment;
  const step: Step = e.state === ENROLLMENT.approved ? "done" : e.state === ENROLLMENT.pending ? "check" : "run";
  return (
    <>
      <StepBar steps={steps(step, t)} />
      {e.state === ENROLLMENT.waiting && (
        <RunPanel enrollment={e} code={code} settings={settings} canWrite={canWrite} />
      )}
      {e.state === ENROLLMENT.pending && <CheckPanel enrollment={e} canWrite={canWrite} />}
      {e.state === ENROLLMENT.approved && <DonePanel enrollment={e} onAgain={onRestart} />}
      {[ENROLLMENT.expired, ENROLLMENT.revoked, ENROLLMENT.rejected, ENROLLMENT.approvalExpired].includes(
        e.state as never,
      ) && <EndedPanel enrollment={e} onAgain={onRestart} />}
    </>
  );
}

function useCopy() {
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setFailed(false);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setFailed(true);
    }
  };
  return { copied, failed, copy };
}

/** Time left before `iso`, or null when it passed. */
function timeLeft(iso: string, now: Date): number | null {
  const end = parseRfc3339(iso);
  if (!end) return null;
  const secs = Math.floor((end.getTime() - now.getTime()) / 1000);
  return secs > 0 ? secs : null;
}

function mmss(secs: number): string {
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

function RunPanel({
  enrollment: e,
  code,
  settings,
  canWrite,
}: {
  enrollment: Enrollment;
  code: string | null;
  settings: Settings;
  canWrite: boolean | undefined;
}) {
  const { t } = useTranslation();
  const now = useNow(1_000);
  const client = useQueryClient();
  const { copied, failed, copy } = useCopy();
  const left = timeLeft(e.expiresAt, now);
  const withdraw = useMutation({
    mutationFn: () => nodesApi.withdrawEnrollment(e.id),
    onSuccess: (r) => client.setQueryData(nodeKeys.enrollment(e.id), r),
  });
  const command = code ? enrollCommand(settings.coreUrl, code, e.group) : null;
  return (
    <section className="hx-panel" aria-labelledby="hx-add-run">
      <div className="hx-panel-head">
        <h2 id="hx-add-run">{t("nodes.addFlow.runTitle")}</h2>
        <StatusPill tone="info">
          {left !== null ? t("nodes.addFlow.validFor", { time: mmss(left) }) : t("nodes.addFlow.expiring")}
        </StatusPill>
      </div>
      <div className="hx-panel-body">
        {command ? (
          <>
            <p className="hx-note">{t("nodes.addFlow.runHint")}</p>
            <pre className="hx-command" translate="no" aria-label={t("nodes.addFlow.command")}>
              {command}
            </pre>
            <div className="hx-row-actions">
              <Button
                variant="secondary"
                icon={copied ? <CheckIcon aria-hidden="true" /> : <CopyIcon aria-hidden="true" />}
                onClick={() => void copy(command.replace(/ \\\n {2}/g, " "))}
              >
                {copied ? t("nodes.addFlow.copied") : t("nodes.addFlow.copy")}
              </Button>
              <span role="status" className="hx-note">
                {failed ? t("nodes.addFlow.copyFailed") : ""}
              </span>
            </div>
            <Notice tone="warn">{t("nodes.addFlow.onlyOnce")}</Notice>
          </>
        ) : (
          <Notice tone="warn">{t("nodes.addFlow.codeGone", { codeId: e.codeId })}</Notice>
        )}
        <dl className="hx-fields">
          <Field label={t("nodes.addFlow.codeId")} mono>
            {e.codeId}
          </Field>
          <Field label={t("nodes.addFlow.expiresAt")}>{at(e.expiresAt)}</Field>
          {e.name && <Field label={t("nodes.fields.name")}>{e.name}</Field>}
          {e.group && <Field label={t("nodes.fields.group")}>{e.group}</Field>}
        </dl>
        <p className="hx-waiting" role="status">
          <span className="hx-waiting-dot" aria-hidden="true" />
          {t("nodes.addFlow.waiting")}
        </p>
        <NodeError error={withdraw.error} />
        <WriteButton
          allowed={canWrite}
          variant="secondary"
          onClick={() => withdraw.mutate()}
          disabled={withdraw.isPending}
        >
          {t("nodes.addFlow.withdraw")}
        </WriteButton>
      </div>
    </section>
  );
}

function CheckPanel({ enrollment: e, canWrite }: { enrollment: Enrollment; canWrite: boolean | undefined }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const now = useNow(1_000);
  const h = e.host!;
  const matches = e.matchingNodes ?? [];
  const d = decide(matches);
  const [choice, setChoice] = useState<string>(""); // node id to replace, or "new"
  const [rejecting, setRejecting] = useState(false);
  const approve = useMutation({
    mutationFn: () =>
      nodesApi.approve(e.id, choice === "new" ? { asNewNode: true } : choice ? { replaceNodeId: choice } : {}),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: nodeKeys.all });
      await client.invalidateQueries({ queryKey: nodeKeys.enrollment(e.id) });
    },
  });
  const needsChoice = d.machine.length > 0 && !choice;
  const deadline = timeLeft(h.claimExpiresAt, now);
  return (
    <section className="hx-panel" aria-labelledby="hx-add-check">
      <div className="hx-panel-head">
        <h2 id="hx-add-check">{t("nodes.addFlow.checkTitle")}</h2>
        <StatusPill tone="ok">{t("nodes.addFlow.claimed", { time: at(h.claimedAt), addr: h.sourceAddr })}</StatusPill>
      </div>
      <div className="hx-panel-body">
        <dl className="hx-fields">
          <Field label={t("nodes.fields.hostname")} mono>
            {h.hostname || "—"}
          </Field>
          <Field label={t("nodes.fields.os")}>{[h.os, h.arch].filter(Boolean).join(" · ") || "—"}</Field>
          <Field label={t("nodes.fields.agentVersion")} mono>
            {h.agentVersion || "—"}
          </Field>
          <Field label={t("nodes.fields.machineId")} mono>
            {h.machineId || t("nodes.addFlow.noMachineId")}
          </Field>
          <Field label={t("nodes.fields.sourceAddr")} mono>
            {h.sourceAddr}
          </Field>
          <Field label={t("nodes.fields.fingerprint")} mono>
            {h.fingerprint}
          </Field>
        </dl>

        <h3 className="hx-dialog-h3">{t("nodes.addFlow.resembles")}</h3>
        {matches.length === 0 ? (
          <p className="hx-note">{t("nodes.addFlow.noMatch")}</p>
        ) : (
          <ul className="hx-match-list">
            {matches.map((m) => (
              <li key={`${m.nodeId}:${m.reason}`}>
                <Link to={nodeHref(m.nodeId)} className="hx-name">
                  {m.name || m.nodeId}
                </Link>
                <StatusPill tone={m.keyRetired ? "danger" : "warn"}>{t(`nodes.addFlow.match.${m.reason}`)}</StatusPill>
                {m.keyRetired && (
                  <span className="hx-sub">
                    {t("nodes.addFlow.keyRetired")}{" "}
                    <span className="hx-mono" translate="no">
                      {t(`nodes.addFlow.retiredReason.${m.keyRetiredReason}`, {
                        defaultValue: m.keyRetiredReason || "—",
                      })}
                    </span>
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}

        {d.blocked ? (
          <Notice tone="danger">{t("nodes.addFlow.blocked")}</Notice>
        ) : d.sameKey ? (
          <p className="hx-note">{t("nodes.addFlow.sameKey", { name: d.sameKey.name || d.sameKey.nodeId })}</p>
        ) : (
          d.machine.length > 0 && (
            <fieldset className="hx-choices">
              <legend>{t("nodes.addFlow.chooseTitle")}</legend>
              {d.machine.map((m) => (
                <label key={m.nodeId} className="hx-choice">
                  <input
                    type="radio"
                    name="hx-approve-choice"
                    checked={choice === m.nodeId}
                    onChange={() => setChoice(m.nodeId)}
                  />
                  <span className="hx-choice-mark" aria-hidden="true" />
                  <span className="hx-choice-text">
                    <strong>{t("nodes.addFlow.replace", { name: m.name || m.nodeId })}</strong>
                    <span className="hx-sub">{t("nodes.addFlow.replaceEffect")}</span>
                  </span>
                </label>
              ))}
              <label className="hx-choice">
                <input
                  type="radio"
                  name="hx-approve-choice"
                  checked={choice === "new"}
                  onChange={() => setChoice("new")}
                />
                <span className="hx-choice-mark" aria-hidden="true" />
                <span className="hx-choice-text">
                  <strong>{t("nodes.addFlow.asNew")}</strong>
                  <span className="hx-sub">{t("nodes.addFlow.asNewEffect")}</span>
                </span>
              </label>
            </fieldset>
          )
        )}

        {deadline !== null && <p className="hx-note">{t("nodes.addFlow.decideBy", { time: at(h.claimExpiresAt) })}</p>}
        <NodeError error={approve.error} />
        <div className="hx-row-actions">
          <WriteButton
            allowed={canWrite}
            variant="primary"
            onClick={() => approve.mutate()}
            disabled={approve.isPending || Boolean(d.blocked) || needsChoice}
          >
            {t("nodes.addFlow.approve")}
          </WriteButton>
          <WriteButton
            allowed={canWrite}
            variant="secondary"
            onClick={() => setRejecting(true)}
            disabled={approve.isPending}
          >
            {t("nodes.addFlow.reject")}
          </WriteButton>
        </div>
      </div>
      {rejecting && <RejectDialog enrollment={e} onClose={() => setRejecting(false)} />}
    </section>
  );
}

function RejectDialog({ enrollment: e, onClose }: { enrollment: Enrollment; onClose: () => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const reject = useMutation({
    mutationFn: () => nodesApi.reject(e.id),
    onSuccess: (r) => {
      client.setQueryData(nodeKeys.enrollment(e.id), r);
      onClose();
    },
  });
  const host = e.host?.hostname || e.host?.fingerprint || "";
  return (
    <Dialog.Root open onOpenChange={(open: boolean) => !open && !reject.isPending && onClose()}>
      <Dialog className="hx-dialog" size="base">
        <Dialog.Title className="hx-dialog-title">{t("nodes.addFlow.rejectTitle", { host })}</Dialog.Title>
        <div className="hx-dialog-body">
          <ul className="hx-consequences">
            <li>{t("nodes.addFlow.rejectEffectHost")}</li>
            <li>{t("nodes.addFlow.rejectEffectCode")}</li>
          </ul>
          <NodeError error={reject.error} />
        </div>
        <div className="hx-dialog-actions">
          <Button variant="secondary" onClick={onClose} disabled={reject.isPending}>
            {t("nodes.cancel")}
          </Button>
          <Button variant="destructive" onClick={() => reject.mutate()} disabled={reject.isPending}>
            {t("nodes.addFlow.rejectConfirm", { host })}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}

function DonePanel({ enrollment: e, onAgain }: { enrollment: Enrollment; onAgain: () => void }) {
  const { t } = useTranslation();
  return (
    <StateBlock
      title={t("nodes.addFlow.approvedTitle", { name: e.name || e.host?.hostname || e.nodeId })}
      detail={<p className="hx-note">{t("nodes.addFlow.approvedDetail")}</p>}
      actions={
        <>
          {e.nodeId && (
            <Link to={nodeHref(e.nodeId)} className="hx-btn-link hx-btn-link--primary">
              {t("nodes.addFlow.openHost")}
            </Link>
          )}
          <Button variant="secondary" onClick={onAgain}>
            {t("nodes.addFlow.addAnother")}
          </Button>
        </>
      }
    />
  );
}

function EndedPanel({ enrollment: e, onAgain }: { enrollment: Enrollment; onAgain: () => void }) {
  const { t } = useTranslation();
  return (
    <StateBlock
      title={t(`nodes.addFlow.ended.${e.state}`)}
      detail={
        <p className="hx-note">
          {t("nodes.addFlow.endedDetail", { codeId: e.codeId, time: at(e.decidedAt ?? e.revokedAt ?? e.expiresAt) })}
        </p>
      }
      actions={
        <Button variant="primary" onClick={onAgain}>
          {t("nodes.addFlow.again")}
        </Button>
      }
    />
  );
}

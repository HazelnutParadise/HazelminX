import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Input, InputArea } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { TrashIcon, UploadSimpleIcon } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { Notice } from "../auth/parts";
import { useNodeList } from "../nodes/hooks";
import {
  LIMITS,
  projectKeys,
  projectsApi,
  ROOTFUL_ENVIRONMENTS,
  type Analysis,
  type Environment,
  type FileInput,
  type Project,
  type Unit,
  type UnitInput,
} from "./api";
import { FindingsPanel, ServicesPanel } from "./EnvironmentContent";
import { PROJECTS_WRITE, useProjectPermission } from "./hooks";
import { GuardedButton, Panel, ProjectError } from "./parts";
import { bytes } from "./format";
import { envHref } from "./paths";

/** One file in the editor. A stored secret file has no content here: it is kept unless replaced. */
interface DraftFile {
  key: string;
  path: string;
  content: string;
  secret: boolean;
  /** A secret file already stored by Core, whose content is never sent to the browser. */
  stored: boolean;
}

const splitList = (s: string) =>
  s
    .split(/[,\n]/)
    .map((x) => x.trim())
    .filter(Boolean);

let seq = 0;
const nextKey = () => `f${++seq}`;

function fromUnit(u: Unit | undefined): DraftFile[] {
  if (!u) return [];
  return (u.files ?? []).map((f) => ({
    key: nextKey(),
    path: f.path ?? "",
    content: f.content ?? "",
    secret: !!f.secret,
    stored: !!f.secret,
  }));
}

/** UTF-8 byte length (Core's limits are in bytes). */
const size = (s: string) => new TextEncoder().encode(s).length;

/**
 * The Compose editor (boards ComposeEditor, UnitConfig): the unit's files as raw text, kept byte for byte so
 * keys HazelminX does not know are never dropped; the -f order, profiles and working directory; the host and
 * engine to deploy to. 檢查 runs Core's validation without saving; 儲存 imports or replaces the unit.
 */
export function UnitEditor({ project: p, env, current }: { project: Project; env: Environment; current?: Unit }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const canWrite = useProjectPermission(PROJECTS_WRITE, p.id);
  const [files, setFiles] = useState<DraftFile[]>(() => fromUnit(current));
  const [mainFile, setMainFile] = useState(current?.mainFile ?? "");
  const [extra, setExtra] = useState((current?.extraFiles ?? []).join(", "));
  const [profiles, setProfiles] = useState((current?.profiles ?? []).join(", "));
  const [workingDir, setWorkingDir] = useState(current?.workingDir ?? "");
  const [nodeId, setNodeId] = useState(current?.target?.nodeId ?? "");
  const [engineEnv, setEngineEnv] = useState(current?.target?.engineEnvironmentId ?? "");
  const [uploaded, setUploaded] = useState(false);
  const [checked, setChecked] = useState<Analysis | null>(null);
  const [readError, setReadError] = useState("");
  const upload = useRef<HTMLInputElement>(null);
  const uploadId = useId();

  const paths = files.map((f) => f.path.trim());
  const main = mainFile || paths[0] || "";
  const total = files.reduce((n, f) => n + size(f.content), 0);
  const problems: string[] = [];
  if (files.length === 0) problems.push(t("projects.editor.problem.noFiles"));
  if (paths.some((x) => !x)) problems.push(t("projects.editor.problem.emptyPath"));
  if (new Set(paths).size !== paths.length) problems.push(t("projects.editor.problem.duplicate"));
  if (files.length > LIMITS.files) problems.push(t("projects.editor.problem.tooMany", { max: LIMITS.files }));
  if (files.some((f) => size(f.content) > LIMITS.fileBytes))
    problems.push(t("projects.editor.problem.fileTooBig", { max: bytes(LIMITS.fileBytes) }));
  if (total > LIMITS.unitBytes) problems.push(t("projects.editor.problem.tooBig", { max: bytes(LIMITS.unitBytes) }));
  const missingExtra = splitList(extra).filter((x) => !paths.includes(x));
  if (missingExtra.length) problems.push(t("projects.editor.problem.extraMissing", { files: missingExtra.join(", ") }));
  if (nodeId && !engineEnv) problems.push(t("projects.editor.problem.noEngine"));

  const input = (): UnitInput => ({
    source: uploaded ? "upload" : current?.source && !uploaded ? current.source : "editor",
    mainFile: main,
    extraFiles: splitList(extra),
    profiles: splitList(profiles),
    workingDir: workingDir.trim() || undefined,
    target: { nodeId, engineEnvironmentId: nodeId ? engineEnv : "" },
    files: files.map((f): FileInput =>
      f.stored && f.content === ""
        ? { path: f.path.trim(), secret: true, unchanged: true }
        : { path: f.path.trim(), content: f.content, secret: f.secret },
    ),
  });

  const check = useMutation({
    mutationFn: () => projectsApi.check(p.id, env.id, input()),
    onSuccess: (r) => setChecked(r.analysis),
  });
  const save = useMutation({
    mutationFn: () =>
      current
        ? projectsApi.replaceUnit(p.id, env.id, current.version ?? 0, input())
        : projectsApi.importUnit(p.id, env.id, input()),
    onSuccess: async (r) => {
      qc.setQueryData(projectKeys.unit(p.id, env.id), r);
      await qc.invalidateQueries({ queryKey: projectKeys.preview(p.id, env.id) });
      void navigate(envHref(p.id, env.id));
    },
  });

  const update = (key: string, patch: Partial<DraftFile>) =>
    setFiles((fs) => fs.map((f) => (f.key === key ? { ...f, ...patch } : f)));

  const readFiles = async (list: FileList | null) => {
    if (!list) return;
    setReadError("");
    const added: DraftFile[] = [];
    for (const file of Array.from(list)) {
      try {
        const text = await file.text();
        added.push({
          key: nextKey(),
          path: file.name,
          content: text,
          secret: /(^|\/)\.env/.test(file.name),
          stored: false,
        });
      } catch {
        setReadError(t("projects.editor.readFailed", { file: file.name }));
      }
    }
    setFiles((fs) => {
      const names = new Set(added.map((a) => a.path));
      return [...fs.filter((f) => !names.has(f.path)), ...added];
    });
    if (added.length) setUploaded(true);
    if (upload.current) upload.current.value = "";
  };

  return (
    <div className="hx-env-stack">
      <Panel
        id="hx-editor-files"
        title={current ? t("projects.editor.titleEdit") : t("projects.editor.titleImport")}
        extra={
          <>
            <input
              ref={upload}
              id={uploadId}
              type="file"
              multiple
              className="hx-visually-hidden"
              onChange={(e) => void readFiles(e.target.files)}
            />
            <Button
              variant="secondary"
              icon={<UploadSimpleIcon aria-hidden="true" />}
              onClick={() => upload.current?.click()}
            >
              {t("projects.editor.upload")}
            </Button>
            <Button
              variant="secondary"
              onClick={() =>
                setFiles((fs) => [
                  ...fs,
                  {
                    key: nextKey(),
                    path: fs.length === 0 ? "compose.yaml" : "",
                    content: "",
                    secret: false,
                    stored: false,
                  },
                ])
              }
            >
              {t("projects.editor.addFile")}
            </Button>
          </>
        }
      >
        <div className="hx-panel-body hx-form">
          <p className="hx-note">{t("projects.editor.rawNote")}</p>
          {readError && <Notice tone="danger">{readError}</Notice>}
          {files.length === 0 && <p className="hx-note">{t("projects.editor.noFiles")}</p>}
          {files.map((f, i) => (
            <fieldset key={f.key} className="hx-file-card">
              <legend className="hx-visually-hidden">{t("projects.editor.fileN", { n: i + 1, path: f.path })}</legend>
              <div className="hx-file-row">
                <Input
                  label={t("projects.editor.path")}
                  value={f.path}
                  className="hx-mono"
                  spellCheck={false}
                  disabled={f.stored}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) => update(f.key, { path: e.target.value })}
                />
                <Checkbox
                  label={t("projects.editor.secret")}
                  checked={f.secret}
                  disabled={f.stored}
                  onCheckedChange={(v: boolean) => update(f.key, { secret: v })}
                />
                <button
                  type="button"
                  className="hx-ib hx-ib--line"
                  aria-label={t("projects.editor.remove", { path: f.path || t("projects.editor.untitled") })}
                  title={t("projects.editor.remove", { path: f.path })}
                  onClick={() => setFiles((fs) => fs.filter((x) => x.key !== f.key))}
                >
                  <TrashIcon size={16} aria-hidden="true" />
                </button>
              </div>
              {f.stored && f.content === "" ? (
                <div className="hx-stored">
                  <span>{t("projects.editor.storedSecret")}</span>
                  <Button variant="secondary" onClick={() => update(f.key, { stored: false })}>
                    {t("projects.editor.replaceSecret")}
                  </Button>
                </div>
              ) : (
                <InputArea
                  label={t("projects.editor.content", { path: f.path || t("projects.editor.untitled") })}
                  value={f.content}
                  rows={Math.min(24, Math.max(6, f.content.split("\n").length + 1))}
                  className="hx-mono hx-code-area"
                  spellCheck={false}
                  onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => update(f.key, { content: e.target.value })}
                />
              )}
              <span className="hx-note">{bytes(size(f.content))}</span>
            </fieldset>
          ))}
        </div>
      </Panel>

      <Panel id="hx-editor-options" title={t("projects.editor.options")}>
        <div className="hx-panel-body hx-form hx-form-grid">
          <Select
            label={t("projects.editor.mainFile")}
            items={paths.filter(Boolean).map((x) => ({ value: x, label: x }))}
            value={main}
            onValueChange={(v: unknown) => setMainFile(typeof v === "string" ? v : "")}
          />
          <Input
            label={t("projects.editor.extraFiles")}
            description={t("projects.editor.extraFilesHint")}
            value={extra}
            className="hx-mono"
            spellCheck={false}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setExtra(e.target.value)}
          />
          <Input
            label={t("projects.editor.profiles")}
            value={profiles}
            className="hx-mono"
            spellCheck={false}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setProfiles(e.target.value)}
          />
          <Input
            label={t("projects.editor.workingDir")}
            value={workingDir}
            placeholder="."
            className="hx-mono"
            spellCheck={false}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setWorkingDir(e.target.value)}
          />
        </div>
      </Panel>

      <TargetPicker
        nodeId={nodeId}
        engineEnv={engineEnv}
        onNode={(v) => {
          setNodeId(v);
          setEngineEnv("");
        }}
        onEngine={setEngineEnv}
      />

      {checked && (
        <>
          <FindingsPanel analysis={checked} title={t("projects.editor.checkResult")} />
          <ServicesPanel analysis={checked} />
        </>
      )}

      <div className="hx-action-bar">
        {problems.length > 0 && (
          <ul className="hx-problems" aria-label={t("projects.editor.problemsLabel")}>
            {problems.map((x) => (
              <li key={x}>{x}</li>
            ))}
          </ul>
        )}
        <ProjectError error={check.error ?? save.error} />
        <div className="hx-action-bar-buttons">
          {current && (
            <LinkButton href={envHref(p.id, env.id)} variant="secondary">
              {t("projects.cancel")}
            </LinkButton>
          )}
          <Button
            variant="secondary"
            disabled={problems.length > 0}
            loading={check.isPending}
            onClick={() => check.mutate()}
          >
            {t("projects.editor.check")}
          </Button>
          <GuardedButton
            allowed={canWrite}
            need={t("projects.needWrite")}
            variant="primary"
            disabled={problems.length > 0}
            loading={save.isPending}
            onClick={() => save.mutate()}
          >
            {current ? t("projects.editor.save") : t("projects.editor.import")}
          </GuardedButton>
        </div>
      </div>
    </div>
  );
}

/** Host and engine environment the unit deploys to: the host's root-owned Docker or Podman engines. */
function TargetPicker({
  nodeId,
  engineEnv,
  onNode,
  onEngine,
}: {
  nodeId: string;
  engineEnv: string;
  onNode: (v: string) => void;
  onEngine: (v: string) => void;
}) {
  const { t } = useTranslation();
  const nodes = useNodeList();
  const engines = useQuery({
    queryKey: projectKeys.engines(nodeId),
    queryFn: () => projectsApi.engines(nodeId),
    enabled: !!nodeId,
  });
  const hosts = (nodes.data?.items ?? []).filter((n) => n.state === "active");
  const envs = (engines.data?.report?.environments ?? []).filter((e) => ROOTFUL_ENVIRONMENTS.includes(e.id ?? ""));
  return (
    <Panel id="hx-editor-target" title={t("projects.editor.target")}>
      <div className="hx-panel-body hx-form hx-form-grid">
        <Select
          label={t("projects.editor.host")}
          items={[
            { value: "", label: t("projects.editor.noHost") },
            ...hosts.map((n) => ({ value: n.id, label: n.name || n.hostname || n.id })),
          ]}
          value={nodeId}
          onValueChange={(v: unknown) => onNode(typeof v === "string" ? v : "")}
        />
        {nodeId && (
          <Select
            label={t("projects.editor.engine")}
            items={[
              { value: "", label: t("projects.editor.chooseEngine") },
              ...envs.map((e) => ({
                value: e.id ?? "",
                label: `${e.kind ?? ""} ${e.version ?? ""} · ${e.id} · ${t(`projects.engineState.${e.state}`, { defaultValue: e.state ?? "" })}`,
              })),
              ...(engineEnv && !envs.some((e) => e.id === engineEnv) ? [{ value: engineEnv, label: engineEnv }] : []),
            ]}
            value={engineEnv}
            onValueChange={(v: unknown) => onEngine(typeof v === "string" ? v : "")}
          />
        )}
        {nodeId && engines.data && !engines.data.reported && (
          <p className="hx-note">{t("projects.editor.engineNotReported")}</p>
        )}
        {nodeId && engines.data?.reported && envs.length === 0 && (
          <p className="hx-note">{t("projects.editor.noRootfulEngine")}</p>
        )}
        {engines.isError && <ProjectError error={engines.error} />}
      </div>
    </Panel>
  );
}

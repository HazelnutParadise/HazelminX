import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeAll, describe, expect, it, vi } from "vite-plus/test";
import { AppProviders } from "../app/App";
import { routes } from "../app/routes";
import { initI18n } from "../i18n";
import { list, node, NOW } from "../nodes/fixtures";
import { apiError, fakeCore } from "../test/fakeCore";

beforeAll(() => {
  initI18n("zh-TW");
  window.matchMedia ??= (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const B = "/management/v1";
const ROLES = {
  roles: [
    { role: "owner", actions: ["*"] },
    { role: "project_admin", actions: ["projects:read", "projects:write", "projects:deploy", "nodes:read"] },
    { role: "viewer", actions: ["projects:read", "nodes:read"] },
  ],
};

function session(role: string) {
  return {
    status: 200,
    body: {
      kind: "session",
      user: {
        id: "u1",
        username: "tim",
        displayName: "",
        status: "active",
        createdAt: NOW,
        updatedAt: NOW,
        version: 1,
      },
      assignments: [{ id: "a1", userId: "u1", role, scope: { kind: "global", id: "*" }, createdAt: NOW }],
      secondFactorEnrollmentRequired: false,
    },
  };
}

const page = (items: unknown[]) => ({
  items,
  page: { limit: 200, total: items.length, capped: false, timedOut: false },
});
const project = {
  id: "prj_1",
  name: "shop",
  displayName: "",
  description: "",
  createdAt: NOW,
  createdBy: "u1",
  updatedAt: NOW,
  version: 1,
};
const prod = {
  id: "env_p",
  projectId: "prj_1",
  name: "production",
  description: "",
  createdAt: NOW,
  createdBy: "u1",
  updatedAt: NOW,
  version: 1,
};
const staging = { ...prod, id: "env_s", name: "staging" };
const E = `${B}/projects/prj_1/environments/env_p`;
const host = node({ id: "n1", name: "hazel-tw-01" });

const unit = {
  id: "unit_1",
  projectId: "prj_1",
  environmentId: "env_p",
  name: "app",
  composeProject: "shop-production-app",
  source: "editor",
  mainFile: "compose.yaml",
  extraFiles: [],
  profiles: [],
  workingDir: "",
  target: { nodeId: "n1", engineEnvironmentId: "env:/var/run/docker.sock" },
  files: [
    { path: "compose.yaml", content: "services: {}\n", secret: false, size: 13, sha256: "ab".repeat(32) },
    { path: ".env", secret: true, size: 20, sha256: "cd".repeat(32) },
  ],
  createdAt: NOW,
  createdBy: "u1",
  updatedAt: NOW,
  version: 3,
};
const analysis = {
  composeProject: "shop-production-app",
  valid: true,
  files: ["compose.yaml"],
  projectDir: ".",
  profiles: [],
  findings: [
    {
      severity: "warning",
      code: "image-latest",
      file: "compose.yaml",
      attribute: "services.web.image",
      message: "uses latest",
    },
    { severity: "high-risk", code: "privileged", attribute: "services.web.privileged", message: "runs privileged" },
  ],
  services: [],
  variables: [],
  networks: [],
  volumes: [],
  configs: [],
  secrets: [],
};
const preview = {
  analysis,
  blockers: [],
  deployable: true,
  needsApproval: true,
  hostChecksPending: ["images-reachable"],
  target: { state: "ready", nodeId: "n1", engineEnvironmentId: "env:/var/run/docker.sock", kind: "docker" },
};
const plan = (over: Record<string, unknown> = {}) => ({
  id: "pln_1",
  kind: "compose.deploy",
  state: "awaiting_approval",
  summary: "deploy shop-production-app on hazel-tw-01 · production",
  digest: "sha256:abc",
  risk: "high",
  executable: false,
  createdAt: NOW,
  updatedAt: NOW,
  expiresAt: NOW,
  requestedBy: { id: "u1", name: "tim" },
  approval: { action: "projects:deploy", required: true, ownerRequired: true, autoApproved: false },
  targets: [
    {
      key: "compose-unit:unit_1",
      kind: "compose-unit",
      id: "unit_1",
      name: "shop-production-app",
      node: "fp",
      nodeName: "hazel-tw-01",
      environment: "production",
      scope: { kind: "project", id: "prj_1" },
      version: 3,
    },
  ],
  checks: [{ id: "privileged:x", status: "high_risk", title: "privileged", detail: "runs privileged" }],
  changes: [],
  affected: [],
  params: {},
  ...over,
});

function core(role = "owner", over: Record<string, unknown> = {}) {
  return fakeCore({
    [`GET ${B}/auth/session`]: session(role),
    [`GET ${B}/roles`]: { status: 200, body: ROLES },
    [`GET ${B}/nodes`]: { status: 200, body: list([host]) },
    [`GET ${B}/projects`]: { status: 200, body: page([project]) },
    [`GET ${B}/projects/prj_1`]: { status: 200, body: { project } },
    [`GET ${B}/projects/prj_1/environments`]: { status: 200, body: page([prod, staging]) },
    [`GET ${E}/compose`]: { status: 200, body: { unit, analysis } },
    [`GET ${E}/compose/preview`]: { status: 200, body: { preview } },
    [`GET ${E}/compose/deployments`]: { status: 200, body: { deployments: [] } },
    [`GET ${B}/projects/prj_1/environments/env_s/compose`]: apiError(404, "NOT_FOUND"),
    ...over,
  } as never);
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AppProviders status={{}}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return router;
}

describe("projects list", () => {
  it("lists projects with links to each environment", async () => {
    core();
    renderAt("/projects");
    const link = await screen.findByRole("link", { name: "production" });
    expect(link).toHaveAttribute("href", "/projects/prj_1/env_p");
    expect(screen.getByRole("link", { name: "shop" })).toHaveAttribute("href", "/projects/prj_1");
  });

  it("creates a project and opens it; a name Core would refuse cannot be sent", async () => {
    const c = core("owner", {
      [`POST ${B}/projects`]: { status: 201, body: { project } },
    });
    const router = renderAt("/projects");
    await userEvent.click(await screen.findByRole("button", { name: "新增專案" }));
    const dialog = await screen.findByRole("dialog", { name: "新增專案" });
    await userEvent.type(within(dialog).getByLabelText("專案名稱"), "Shop-");
    expect(within(dialog).getByRole("button", { name: "建立專案" })).toBeDisabled();
    await userEvent.clear(within(dialog).getByLabelText("專案名稱"));
    await userEvent.type(within(dialog).getByLabelText("專案名稱"), "shop");
    await userEvent.click(within(dialog).getByRole("button", { name: "建立專案" }));
    await waitFor(() => expect(router.state.location.pathname).toMatch(/^\/projects\/prj_1/));
    expect(c.callsTo(`POST ${B}/projects`)[0]!.body).toEqual({ name: "shop", displayName: "", description: "" });
  });

  it("locks 新增專案 for an account without projects:write", async () => {
    core("viewer");
    renderAt("/projects");
    await screen.findByRole("link", { name: "shop" });
    expect(screen.getAllByRole("button", { name: /新增專案/ })[0]).toBeDisabled();
  });
});

describe("project page", () => {
  it("opens the first environment, frames the environments as tabs, and shows the unit", async () => {
    core();
    const router = renderAt("/projects/prj_1");
    await waitFor(() => expect(router.state.location.pathname).toBe("/projects/prj_1/env_p"));
    const tabs = await screen.findByRole("navigation", { name: "環境" });
    expect(within(tabs).getByRole("link", { name: "production" })).toHaveAttribute("aria-current", "page");
    expect(await screen.findByText("shop-production-app")).toBeInTheDocument();
    // Findings: the high-risk one first, then the warning.
    const findings = screen.getByRole("region", { name: "Compose 檢查" });
    const rows = within(findings).getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("高風險");
    expect(rows[1]).toHaveTextContent("警告");
    // A secret file is listed without content.
    expect(screen.getByRole("region", { name: /^檔案/ })).toHaveTextContent(".env機密");
  });

  it("an environment without a unit opens the editor; no host means no target sent", async () => {
    const c = core("owner", {
      [`POST ${B}/projects/prj_1/environments/env_s/compose/check`]: {
        status: 200,
        body: { analysis: { ...analysis, findings: [] } },
      },
    });
    renderAt("/projects/prj_1/env_s");
    expect(await screen.findByRole("heading", { name: "匯入 Compose" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "匯入" })).toBeDisabled();
    expect(screen.getByText("至少要有一個檔案")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "新增檔案" }));
    await userEvent.type(screen.getByLabelText("compose.yaml 的內容"), "services: {{}}");
    await userEvent.click(screen.getByRole("button", { name: "檢查" }));
    await waitFor(() => expect(c.callsTo(`POST ${B}/projects/prj_1/environments/env_s/compose/check`)).toHaveLength(1));
    const body = c.callsTo(`POST ${B}/projects/prj_1/environments/env_s/compose/check`)[0]!.body as Record<
      string,
      unknown
    >;
    expect(body).toMatchObject({ name: "app", mainFile: "compose.yaml", target: null, source: "editor" });
    expect(await screen.findByText("沒有要處理的項目")).toBeInTheDocument();
  });

  it("editing keeps a stored secret file unchanged and replaces with If-Match of the unit's version", async () => {
    const c = core("owner", {
      [`PUT ${E}/compose`]: { status: 200, body: { unit: { ...unit, version: 4 }, analysis } },
    });
    renderAt("/projects/prj_1/env_p/compose");
    expect(await screen.findByRole("heading", { name: "編輯 Compose" })).toBeInTheDocument();
    expect(screen.getByText(/已存的機密檔案/)).toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: "儲存" }));
    await waitFor(() => expect(c.callsTo(`PUT ${E}/compose`)).toHaveLength(1));
    const files = (c.callsTo(`PUT ${E}/compose`)[0]!.body as { files: unknown[] }).files;
    expect(files).toEqual([
      { path: "compose.yaml", content: "services: {}\n", secret: false },
      { path: ".env", secret: true, unchanged: true },
    ]);
    const init = vi.mocked(fetch).mock.calls.find((x) => (x[1] as RequestInit)?.method === "PUT")![1] as RequestInit;
    expect(new Headers(init.headers).get("If-Match")).toBe('"v3"');
  });
});

describe("deploying", () => {
  it("a plan that needs the owner shows a locked 批准 to anyone else", async () => {
    core("project_admin", { [`GET ${B}/change-plans/pln_1`]: { status: 200, body: { plan: plan() } } });
    renderAt("/projects/prj_1/env_p?plan=pln_1");
    const panel = await screen.findByRole("region", { name: /^部署計畫/ });
    expect(await within(panel).findByText("等待批准")).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: /批准/ })).toBeDisabled();
    expect(within(panel).getByText("含高風險設定，只有擁有者能批准")).toBeInTheDocument();
  });

  it("approval asks for the environment name, then runs the plan with its digest", async () => {
    let state = plan();
    const c = core("owner", {
      [`GET ${B}/change-plans/pln_1`]: () => ({ status: 200, body: { plan: state } }),
      [`POST ${B}/change-plans/pln_1/approve`]: () => {
        state = plan({
          state: "approved",
          executable: true,
          approval: { ...plan().approval, approvedBy: { id: "u1", name: "tim" } },
        });
        return { status: 200, body: { plan: state } };
      },
      [`POST ${B}/change-plans/pln_1/execute`]: () => {
        state = plan({
          state: "executed",
          execution: { at: NOW, by: { id: "u1", name: "tim" }, jobId: "job_1", requestId: "r" },
        });
        return { status: 202, body: { jobId: "job_1", deduplicated: false } };
      },
      [`GET ${B}/jobs/job_1`]: {
        status: 200,
        body: {
          job: {
            id: "job_1",
            state: "failed",
            progress: 50,
            steps: [
              { index: 0, name: "check the host", status: "done" },
              {
                index: 1,
                name: "run docker compose up",
                status: "failed",
                error: { code: "EXECUTOR_ACTION_FAILED", summary: "up exited 17" },
              },
            ],
          },
        },
      },
    });
    renderAt("/projects/prj_1/env_p?plan=pln_1");
    const panel = await screen.findByRole("region", { name: /^部署計畫/ });
    await userEvent.click(await within(panel).findByRole("button", { name: "批准" }));
    const dialog = await screen.findByRole("dialog", { name: /批准部署 shop-production-app 到 hazel-tw-01/ });
    const submit = within(dialog).getByRole("button", { name: "批准 production" });
    expect(submit).toBeDisabled();
    expect(within(dialog).getByText("runs privileged")).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("輸入環境名稱 production 確認"), "production");
    await userEvent.click(submit);
    await waitFor(() =>
      expect(c.callsTo(`POST ${B}/change-plans/pln_1/approve`)[0]!.body).toEqual({ digest: "sha256:abc" }),
    );
    await userEvent.click(await within(panel).findByRole("button", { name: "部署到 hazel-tw-01" }));
    await waitFor(() => expect(c.callsTo(`POST ${B}/change-plans/pln_1/execute`)).toHaveLength(1));
    expect(c.callsTo(`POST ${B}/change-plans/pln_1/execute`)[0]!.body).toMatchObject({ digest: "sha256:abc" });
    const job = await screen.findByRole("region", { name: /^部署進度/ });
    expect(await within(job).findByText(/不會自動回滾/)).toBeInTheDocument();
  });

  it("a retried run keeps its request id, and a conflict reads the plan again instead of failing", async () => {
    let calls = 0;
    const c = core("owner", {
      [`GET ${B}/change-plans/pln_1`]: () => ({
        status: 200,
        body: { plan: plan({ state: "approved", executable: true }) },
      }),
      [`POST ${B}/change-plans/pln_1/execute`]: () =>
        ++calls === 1 ? apiError(503, "PLATFORM_DEGRADED") : apiError(409, "REQUEST_ID_CONFLICT"),
    });
    renderAt("/projects/prj_1/env_p?plan=pln_1");
    const panel = await screen.findByRole("region", { name: /^部署計畫/ });
    const run = await within(panel).findByRole("button", { name: "部署到 hazel-tw-01" });
    await userEvent.click(run);
    await waitFor(() => expect(c.callsTo(`POST ${B}/change-plans/pln_1/execute`)).toHaveLength(1));
    await userEvent.click(await within(panel).findByRole("button", { name: "部署到 hazel-tw-01" }));
    await waitFor(() => expect(c.callsTo(`POST ${B}/change-plans/pln_1/execute`)).toHaveLength(2));
    const ids = c
      .callsTo(`POST ${B}/change-plans/pln_1/execute`)
      .map((x) => (x.body as { requestId: string }).requestId);
    expect(ids[0]).toBe(ids[1]);
    await waitFor(() => expect(c.callsTo(`GET ${B}/change-plans/pln_1`).length).toBeGreaterThan(1));
    expect(within(panel).queryByText(/REQUEST_ID_CONFLICT/)).toBeNull();
  });

  it("a host check that changed nothing does not warn about a partial update", async () => {
    core("owner", {
      [`GET ${B}/jobs/job_c`]: {
        status: 200,
        body: {
          job: {
            id: "job_c",
            state: "failed",
            progress: 0,
            steps: [
              {
                index: 0,
                name: "check the host",
                status: "failed",
                error: { code: "HOST_CHECK_FAILED", summary: "1 failed", nothingChanged: true },
              },
            ],
          },
        },
      },
    });
    renderAt("/projects/prj_1/env_p?check=job_c");
    const job = await screen.findByRole("region", { name: /^主機預檢/ });
    expect(await within(job).findByText("主機沒有任何變更。")).toBeInTheDocument();
    expect(within(job).queryByText(/不會自動回滾/)).toBeNull();
  });

  it("a failed deployment row says it was not rolled back", async () => {
    core("owner", {
      [`GET ${E}/compose/deployments`]: {
        status: 200,
        body: {
          deployments: [
            {
              id: "dep_1",
              unitId: "unit_1",
              unitVersion: 3,
              composeProject: "shop-production-app",
              revision: "job_1",
              manifestDigest: "x",
              nodeId: "n1",
              engineEnvironmentId: "env:/var/run/docker.sock",
              jobId: "job_1",
              state: "failed",
              errorCode: "EXECUTOR_ACTION_FAILED",
              errorMessage: "up exited 17",
              result: null,
              createdBy: "u1",
              startedAt: NOW,
              finishedAt: NOW,
            },
            {
              id: "dep_0",
              unitId: "unit_1",
              unitVersion: 2,
              composeProject: "shop-production-app",
              revision: "job_0",
              manifestDigest: "x",
              nodeId: "n1",
              engineEnvironmentId: "env:/var/run/docker.sock",
              jobId: "job_0",
              state: "succeeded",
              result: { containers: [{ name: "shop-web-1", state: "running" }] },
              createdBy: "u1",
              startedAt: NOW,
              finishedAt: NOW,
            },
          ],
        },
      },
    });
    renderAt("/projects/prj_1/env_p");
    const panel = await screen.findByRole("region", { name: /^部署紀錄/ });
    const rows = (await within(panel).findAllByRole("row")).slice(1);
    expect(rows[0]).toHaveTextContent("失敗");
    expect(rows[0]).toHaveTextContent("沒有自動回滾，容器可能停在部分更新的狀態");
    expect(rows[1]).toHaveTextContent("shop-web-1 · running");
    expect(rows[1]).toHaveTextContent("hazel-tw-01");
  });
});

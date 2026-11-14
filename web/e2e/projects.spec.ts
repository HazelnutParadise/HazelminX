import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { expect, request, test, type Page } from "@playwright/test";
import { BASE_URL, call, E2E_DIR, OWNER_STATE } from "./core";
import { scanIssues, seedPrefs, watchErrors } from "./scan";

/**
 * Projects, environments and Compose units against a real Core (ticket 044.3). Without an agent: create a
 * project and environments, import a unit, read its findings and preview, edit it keeping keys HazelminX does
 * not know. With HAZELMINX_E2E_AGENT_BIN and HAZELMINX_E2E_PRIVILEGED_BIN (Linux builds) and the image
 * hzx-compose-host:trixie (hzx-ledger-test plus docker.io, docker-cli, docker-compose): a real host running
 * dockerd, the privileged executor and the agent as uid 65534; the unit is checked on the host, deployed
 * through an approved change plan, and a second deployment that fails shows what it leaves behind.
 */

const uniq = (p: string) => `${p}-${randomBytes(3).toString("hex")}`;
const SHOTS = join(E2E_DIR, "shots");

/**
 * Page errors, minus the 404 Core answers for an environment that has no Compose unit yet (GET …/compose is
 * how the page learns that), which Chromium logs as a failed resource. Any other 404 still counts.
 */
function watchPageErrors(page: Page): () => string[] {
  const errors = watchErrors(page);
  let expected = 0;
  page.on("response", (r) => {
    if (r.status() === 404 && r.request().method() === "GET" && /\/environments\/[^/]+\/compose$/.test(r.url()))
      expected++;
  });
  return () => {
    let skip = expected;
    return errors.filter((e) => !(e.includes("status of 404") && skip-- > 0));
  };
}

async function scanScreen(page: Page, errors: () => string[], label: string, shot?: string) {
  for (const [width, mode] of [
    [1440, "dark"],
    [360, "dark"],
    [1440, "light"],
    [360, "light"],
  ] as const) {
    await page.setViewportSize({ width, height: 900 });
    await page.evaluate((m) => document.documentElement.setAttribute("data-mode", m), mode);
    expect(await scanIssues(page), `${label} @${width} ${mode}`).toEqual([]);
    if (shot) {
      mkdirSync(SHOTS, { recursive: true });
      await page.evaluate(() => document.querySelectorAll(".hx-table-wrap").forEach((w) => (w.scrollLeft = 0)));
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.screenshot({ path: join(SHOTS, `${shot}-${width}-${mode}.png`), fullPage: true });
    }
  }
  await page.evaluate(() => document.documentElement.setAttribute("data-mode", "dark"));
  await page.setViewportSize({ width: 1440, height: 900 });
  expect(errors(), `${label}: page and console errors`).toEqual([]);
}

const COMPOSE = `# kept byte for byte
services:
  web:
    image: busybox:1.36
    command: ["sleep", "infinity"]
    privileged: true
    environment:
      DB_PASSWORD: \${DB_PASSWORD:?set DB_PASSWORD}
x-team-notes:
  owner: ops
`;

async function createProject(page: Page, name: string) {
  await page.goto("/projects");
  await page.getByRole("button", { name: "新增專案" }).first().click();
  const dialog = page.getByRole("dialog", { name: "新增專案" });
  await dialog.getByLabel("專案名稱").fill(name);
  await dialog.getByRole("button", { name: "建立專案" }).click();
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
}

async function addEnvironment(page: Page, env: string) {
  await page.getByRole("button", { name: "環境", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: /的環境$/ });
  await dialog.getByLabel("環境名稱").fill(env);
  await dialog.getByRole("button", { name: "建立環境" }).click();
  await expect(page.getByRole("navigation", { name: "環境" }).getByRole("link", { name: env })).toHaveAttribute(
    "aria-current",
    "page",
  );
}

async function typeFile(page: Page, content: string) {
  await page.getByRole("button", { name: "新增檔案" }).click();
  await expect(page.getByLabel("路徑")).toHaveValue("compose.yaml");
  await page.getByLabel("compose.yaml 的內容").fill(content);
}

test.describe.configure({ mode: "serial" });

test.beforeEach(async ({ page }) => {
  await seedPrefs(page, { mode: "dark", lang: "zh-TW" });
  await page.setViewportSize({ width: 1440, height: 900 });
});

test("projects: create, add environments, import a unit, read findings, edit without losing unknown keys", async ({
  page,
}) => {
  const errors = watchPageErrors(page);
  const name = uniq("shop");
  await page.goto("/projects");
  await expect(page.getByRole("heading", { level: 1, name: "專案與服務" })).toBeVisible();
  await createProject(page, name);
  await expect(page.getByText("這個專案還沒有環境")).toBeVisible();
  await scanScreen(page, errors, "project without environments", "project-empty");

  await addEnvironment(page, "staging");
  await addEnvironment(page, "production");
  await expect(page.getByRole("heading", { name: "匯入 Compose" })).toBeVisible();

  await typeFile(page, COMPOSE);
  await page.getByRole("button", { name: "檢查", exact: true }).click();
  const result = page.getByRole("region", { name: "檢查結果（尚未儲存）" });
  await expect(result.getByText("擋下", { exact: true })).toBeVisible();
  await expect(result.getByText("DB_PASSWORD").first()).toBeVisible();
  await scanScreen(page, errors, "editor with check result", "editor");

  await page.getByRole("button", { name: "匯入", exact: true }).click();
  await expect(page.getByRole("region", { name: /^檔案 · 1/ })).toBeVisible();
  await expect(
    page.getByRole("region", { name: "Compose 檢查" }).getByText("擋下", { exact: true }).first(),
  ).toBeVisible();
  const preview = page.getByRole("region", { name: "部署預檢" });
  await expect(preview.getByText("還沒選主機").first()).toBeVisible();
  await expect(preview.getByRole("button", { name: "建立部署計畫" })).toBeDisabled();
  await scanScreen(page, errors, "unit with findings", "unit");

  // Fix the missing variable in the raw editor; the comment and the x- key stay as written.
  await page.getByRole("link", { name: "編輯 Compose" }).click();
  const fixed = COMPOSE.replace("${DB_PASSWORD:?set DB_PASSWORD}", "${DB_PASSWORD:-changeme}");
  await page.getByLabel("compose.yaml 的內容").fill(fixed);
  await page.getByRole("button", { name: "儲存", exact: true }).click();
  await expect(page.getByText("第 2 版")).toBeVisible();
  // Readable now: the privileged service is a high-risk finding and the deployment needs the owner's approval.
  await expect(
    page.getByRole("region", { name: "Compose 檢查" }).getByText("高風險", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByRole("region", { name: "服務 · 1" })).toBeVisible();

  const owner = await request.newContext({ baseURL: BASE_URL, storageState: OWNER_STATE });
  try {
    const projects = (await call(owner, "GET", "/projects")) as { items: { id: string; name: string }[] };
    const p = projects.items.find((x) => x.name === name)!;
    const envs = (await call(owner, "GET", `/projects/${p.id}/environments`)) as {
      items: { id: string; name: string }[];
    };
    const env = envs.items.find((e) => e.name === "production")!;
    const unit = (await call(owner, "GET", `/projects/${p.id}/environments/${env.id}/compose`)) as {
      unit: { files: { path: string; content: string }[] };
    };
    expect(unit.unit.files[0]!.content).toBe(fixed);
  } finally {
    await owner.dispose();
  }

  await page.goto("/projects");
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  await scanScreen(page, errors, "project list", "projects");
});

const AGENT = process.env.HAZELMINX_E2E_AGENT_BIN;
const PRIV = process.env.HAZELMINX_E2E_PRIVILEGED_BIN;

const DEPLOY_COMPOSE = `services:
  web:
    image: busybox:1.36
    command: ["sleep", "infinity"]
    privileged: true
`;
const FAILING_COMPOSE = `services:
  web:
    image: busybox:1.36
    command: ["sleep", "infinity"]
    privileged: true
  api:
    command: ["sleep", "infinity"]
    depends_on: [web]
    build:
      context: .
      dockerfile: Dockerfile
`;

test.describe("with a real host running Docker", () => {
  test.skip(!AGENT || !PRIV, "set HAZELMINX_E2E_AGENT_BIN and HAZELMINX_E2E_PRIVILEGED_BIN to Linux builds");
  test.setTimeout(600_000);
  let host: ChildProcess | undefined;
  let nodeId = "";
  const container = `hzx-e2e-compose-${randomBytes(3).toString("hex")}`;
  test.afterAll(async () => {
    // Stop managing the host before its container goes away, so later specs do not see a host that stopped
    // answering in the host bar and the host list.
    if (nodeId) {
      const owner = await request.newContext({ baseURL: BASE_URL, storageState: OWNER_STATE });
      await call(owner, "POST", `/nodes/${nodeId}/remove`, { mode: "unmanage" }).catch(() => undefined);
      await owner.dispose();
    }
    host?.kill();
    try {
      execFileSync("docker", ["rm", "-f", container], { stdio: "ignore" });
    } catch {
      /* already gone */
    }
  });

  test("check the host, deploy through an approved plan, then a failed deployment says what it left", async ({
    page,
  }) => {
    const errors = watchPageErrors(page);
    const hostName = uniq("compose-host");
    await page.goto("/nodes/new");
    await page.getByLabel("顯示名稱").fill(hostName);
    await page.getByRole("button", { name: "產生註冊碼" }).click();
    const command = page.getByLabel("註冊指令");
    await expect(command).toBeVisible();
    const code = /HZX-[0-9A-Z]{4}(?:-[0-9A-Z]{4}){3}/.exec((await command.textContent()) ?? "")![0];
    const port = (process.env.HAZELMINX_E2E_AGENT_ADDR ?? "0.0.0.0:19891").split(":").pop();
    const url = `https://host.docker.internal:${port}`;
    const asAgent = "setpriv --reuid 65534 --regid 65534 --clear-groups";
    const script = [
      "set -e",
      "dockerd --host unix:///var/run/docker.sock >/tmp/dockerd.log 2>&1 &",
      "mkdir -p /w /var/lib/hazelminx && chmod 755 /w && cp /a /w/agent && cp /p /w/priv && chmod 755 /w/agent /w/priv",
      "mkdir -p /w/state && chown 65534:65534 /w/state",
      `${asAgent} /w/agent --config /nonexistent.toml --state-dir /w/state --core-url ${url} enroll --core-url ${url} --code ${code}`,
      `printf 'compose_spool_dir = "/w/state/compose-spool"\\n' > /w/priv.toml`,
      "/w/priv --config /w/priv.toml --socket /w/priv.sock --issuer-key /w/state/identity/privileged-issuer.pub --allowed-uid 65534 --ledger-dir /w/ledger --log-format text run &",
      "for i in $(seq 100); do [ -S /w/priv.sock ] && break; sleep 0.1; done; chown 0:65534 /w/priv.sock",
      `printf 'privileged_socket = "/w/priv.sock"\\n' > /w/agent.toml`,
      `exec ${asAgent} /w/agent --config /w/agent.toml --state-dir /w/state --core-url ${url} --log-format text run`,
    ].join("\n");
    const machineId = join(E2E_DIR, `${container}.machine-id`);
    mkdirSync(E2E_DIR, { recursive: true });
    writeFileSync(machineId, `${randomBytes(16).toString("hex")}\n`);
    host = spawn(
      "docker",
      [
        "run",
        "--rm",
        "--privileged",
        "--name",
        container,
        "-v",
        `${AGENT}:/a:ro`,
        "-v",
        `${PRIV}:/p:ro`,
        "-v",
        `${machineId}:/etc/machine-id:ro`,
        "hzx-compose-host:trixie",
        "sh",
        "-c",
        script,
      ],
      { stdio: "inherit" },
    );
    await page.getByRole("button", { name: "是這台，加入" }).click({ timeout: 90_000 });
    await expect(page.getByText(`${hostName} 已加入`)).toBeVisible();
    // Docker Hub limits anonymous pulls: hand the host the one image the units use, from this machine.
    await expect
      .poll(
        () => {
          try {
            execFileSync("sh", ["-c", `docker save busybox:1.36 | docker exec -i ${container} docker load`], {
              stdio: "ignore",
            });
            return true;
          } catch {
            return false;
          }
        },
        { timeout: 60_000, intervals: [2_000] },
      )
      .toBe(true);

    // The engine report: ask for a fresh one until the host's Docker answers.
    const owner = await request.newContext({ baseURL: BASE_URL, storageState: OWNER_STATE });
    const nodes = (await call(owner, "GET", `/nodes?q=${hostName}`)) as { items: { id: string; name: string }[] };
    nodeId = nodes.items.find((n) => n.name === hostName)!.id;
    await expect
      .poll(
        async () => {
          await call(owner, "POST", `/nodes/${nodeId}/engines/refresh`).catch(() => undefined);
          const r = (await call(owner, "GET", `/nodes/${nodeId}/engines`)) as {
            report?: { environments?: { id: string; state: string }[] };
          };
          // Debian links /var/run to /run, so the engine may be reported under either path.
          const env = r.report?.environments?.find((e) => /^env:(\/var)?\/run\/docker\.sock$/.test(e.id));
          return env?.state ?? "none";
        },
        { timeout: 120_000, intervals: [3_000] },
      )
      .toBe("running");
    await owner.dispose();

    const project = uniq("deploy");
    await createProject(page, project);
    await addEnvironment(page, "production");
    await typeFile(page, DEPLOY_COMPOSE);
    await page.getByRole("combobox", { name: "主機" }).click();
    await page.getByRole("option", { name: hostName }).click();
    await page.getByRole("combobox", { name: "引擎環境" }).click();
    await page.getByRole("option", { name: /env:(\/var)?\/run\/docker\.sock/ }).click();
    await page.getByRole("button", { name: "匯入", exact: true }).click();

    const preview = page.getByRole("region", { name: "部署預檢" });
    await expect(preview.getByText("就緒")).toBeVisible({ timeout: 30_000 });
    await expect(preview.getByText("含高風險設定，需要擁有者批准")).toBeVisible();

    await preview.getByRole("button", { name: "主機預檢" }).click();
    const check = page.getByRole("region", { name: /^主機預檢/ }).last();
    await expect(check.getByText("成功").first()).toBeVisible({ timeout: 180_000 });

    await preview.getByRole("button", { name: "建立部署計畫" }).click();
    const plan = page.getByRole("region", { name: /^部署計畫/ });
    await expect(plan.getByText("等待批准").first()).toBeVisible();
    await expect(plan.getByText("高風險", { exact: true }).first()).toBeVisible();
    await scanScreen(page, errors, "plan awaiting approval", "plan");

    await plan.getByRole("button", { name: "批准", exact: true }).click();
    const confirm = page.getByRole("dialog", { name: /^批准部署/ });
    await expect(confirm.getByText(hostName).first()).toBeVisible();
    await expect(confirm.getByRole("button", { name: "批准 production" })).toBeDisabled();
    await scanScreen(page, errors, "approval dialog", "approve");
    await confirm.getByLabel("輸入環境名稱 production 確認").fill("production");
    await confirm.getByRole("button", { name: "批准 production" }).click();
    await expect(plan.getByText("已批准").first()).toBeVisible();
    await plan.getByRole("button", { name: `部署到 ${hostName}` }).click();

    const job = page.getByRole("region", { name: /^部署進度/ });
    await expect(job.getByText("成功").first()).toBeVisible({ timeout: 300_000 });
    const deployments = page.getByRole("region", { name: /^部署紀錄/ });
    await expect(deployments.getByText("成功", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(deployments.getByText(/web.* · running/)).toBeVisible();
    await scanScreen(page, errors, "deployed", "deployed");

    // A second revision whose build fails: the deployment row says failed and that nothing is rolled back.
    await page.getByRole("link", { name: "編輯 Compose" }).click();
    await page.getByLabel("compose.yaml 的內容").fill(FAILING_COMPOSE);
    await page.getByRole("button", { name: "新增檔案" }).click();
    await page.getByLabel("路徑").last().fill("Dockerfile");
    await page.getByLabel("Dockerfile 的內容").fill("FROM scratch\nRUN exit 1\n");
    await page.getByRole("button", { name: "儲存", exact: true }).click();
    await expect(page.getByText("第 2 版")).toBeVisible();
    await preview.getByRole("button", { name: "建立部署計畫" }).click();
    await plan.getByRole("button", { name: "批准", exact: true }).click();
    await confirm.getByLabel("輸入環境名稱 production 確認").fill("production");
    await confirm.getByRole("button", { name: "批准 production" }).click();
    await plan.getByRole("button", { name: `部署到 ${hostName}` }).click();
    await expect(job.getByText("失敗").first()).toBeVisible({ timeout: 300_000 });
    await expect(job.getByText(/不會自動回滾/)).toBeVisible();
    await expect(deployments.getByText("失敗", { exact: true })).toBeVisible({ timeout: 30_000 });
    await expect(deployments.getByText("沒有自動回滾，容器可能停在部分更新的狀態")).toBeVisible();
    await scanScreen(page, errors, "failed deployment", "failed");
  });
});

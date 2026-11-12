import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeAll, describe, expect, it, vi } from "vite-plus/test";
import { AppProviders } from "../app/App";
import { routes } from "../app/routes";
import { initI18n } from "../i18n";
import { apiError, fakeCore } from "../test/fakeCore";
import { claimedHost, enrollment, list, node, NOW, ROLES } from "./fixtures";

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
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const B = "/management/v1";
const user = {
  id: "u1",
  username: "tim",
  displayName: "",
  status: "active",
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
};

function session(role = "owner") {
  return {
    status: 200,
    body: {
      kind: "session",
      user,
      assignments: [{ id: "a1", userId: "u1", role, scope: { kind: "global", id: "*" }, createdAt: NOW }],
      secondFactorEnrollmentRequired: false,
    },
  };
}

const tw = node({ id: "tw-01", name: "hazel-tw-01", tags: ["production"], group: "taipei" });
const jp = node({
  id: "jp-02",
  name: "hazel-jp-02",
  connection: {
    state: "offline",
    lastContactAt: "2026-11-12T03:41:18Z",
    clockSkewSeconds: null,
    clockSkewWarning: false,
  } as never,
  pendingSync: ["certificate-rotation"],
  certificate: { serial: "9", notAfter: "2026-11-24T00:00:00Z", state: "expiring", expiresInDays: 12 } as never,
});
const mini = node({
  id: "mini",
  name: "lab-mini",
  tags: ["lab"],
  connection: { state: "offline", lastContactAt: "2026-11-12T03:00:00Z" } as never,
  maintenance: { enabled: true, reason: "disk swap", since: "2026-11-12T02:00:00Z" } as never,
});

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AppProviders status={{}}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return router;
}

function coreWith(nodes = [tw, jp, mini], role = "owner") {
  return fakeCore({
    [`GET ${B}/auth/session`]: session(role),
    [`GET ${B}/roles`]: { status: 200, body: ROLES },
    [`GET ${B}/nodes`]: { status: 200, body: list(nodes) },
    ...Object.fromEntries(nodes.map((n) => [`GET ${B}/nodes/${n.id}`, { status: 200, body: { node: n } }])),
  });
}

const h1 = (name: string) => screen.findByRole("heading", { level: 1, name });

describe("host list", () => {
  it("shows each host's status, last contact, certificate and what has not reached it", async () => {
    coreWith();
    renderAt("/nodes");
    await h1("主機");
    const table = await screen.findByRole("table");
    const row = (name: string) => within(table).getByRole("link", { name }).closest("tr")!;
    expect(within(row("hazel-tw-01")).getByText("連線中")).toBeInTheDocument();
    expect(within(row("hazel-jp-02")).getByText("沒有回應")).toBeInTheDocument();
    expect(within(row("hazel-jp-02")).getByText("通道中斷")).toBeInTheDocument();
    expect(within(row("hazel-jp-02")).getByText("即將到期 · 剩 12 天")).toBeInTheDocument();
    expect(within(row("hazel-jp-02")).getByText("新憑證")).toBeInTheDocument();
    expect(within(row("lab-mini")).getByText("維護中")).toBeInTheDocument();
    expect(within(row("hazel-tw-01")).getByText("production")).toBeInTheDocument();
    expect(within(row("hazel-jp-02")).getByRole("time")).toHaveAttribute("datetime", "2026-11-12T03:41:18Z");
    expect(document.body.textContent).not.toMatch(/關機|已停止/);
  });

  it("feeds the shell's host bar with hosts that stopped answering, leaving maintenance out", async () => {
    coreWith();
    renderAt("/");
    const bar = await screen.findByRole("status", { name: "主機狀態" });
    expect(within(bar).getByText("1 台主機沒有回應")).toBeInTheDocument();
    expect(within(bar).getByRole("link", { name: "hazel-jp-02" })).toHaveAttribute("href", "/nodes/jp-02");
    expect(within(bar).queryByText("lab-mini")).toBeNull();
    expect(within(bar).getByRole("link", { name: "全部主機" })).toHaveAttribute("href", "/nodes?status=unreachable");
  });

  it("has no host bar when every host answers", async () => {
    coreWith([tw]);
    renderAt("/");
    await screen.findByRole("heading", { level: 1 });
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(expect.stringContaining("/nodes?limit=200"), expect.anything()),
    );
    expect(screen.queryByRole("status", { name: "主機狀態" })).toBeNull();
  });

  it("filters by status from the URL and by search, keeping the conditions in the URL", async () => {
    coreWith();
    const router = renderAt("/nodes?status=unreachable");
    const table = await screen.findByRole("table");
    await waitFor(() => expect(within(table).getAllByRole("link")).toHaveLength(1));
    expect(within(table).getByRole("link", { name: "hazel-jp-02" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "沒有回應" })).toHaveAttribute("aria-pressed", "true");

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "維護中" }));
    await waitFor(() => expect(router.state.location.search).toBe("?status=maintenance"));
    await waitFor(() =>
      expect(within(screen.getByRole("table")).getByRole("link", { name: "lab-mini" })).toBeInTheDocument(),
    );

    await u.type(screen.getByLabelText("搜尋"), "lab");
    await waitFor(() => expect(router.state.location.search).toBe("?q=lab&status=maintenance"));
    const urls = () => (fetch as unknown as { mock: { calls: [string][] } }).mock.calls.map((c) => String(c[0]));
    await waitFor(() => expect(urls()).toContain(`${B}/nodes?q=lab&limit=200`));
  });

  it("says which conditions matched nothing and clears them", async () => {
    coreWith();
    const router = renderAt("/nodes?status=never&tag=lab");
    expect(await screen.findByText("沒有符合條件的主機")).toBeInTheDocument();
    expect(screen.getByText("目前的條件：從未連線、標籤 lab")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "清除篩選" }));
    await waitFor(() => expect(router.state.location.search).toBe(""));
    expect(await screen.findByRole("link", { name: "hazel-tw-01" })).toBeInTheDocument();
  });

  it("shows the empty state with the add button when there are no hosts", async () => {
    coreWith([]);
    renderAt("/nodes");
    expect(await screen.findByText("還沒有主機")).toBeInTheDocument();
    const add = screen.getAllByRole("link", { name: "新增主機" });
    expect(add.length).toBeGreaterThan(0);
    expect(add[0]).toHaveAttribute("href", "/nodes/new");
  });

  it("disables writes with the reason for an account without nodes:write", async () => {
    coreWith([tw], "auditor");
    renderAt("/nodes");
    await screen.findByRole("table");
    const add = await screen.findByRole("button", { name: /新增主機/ });
    expect(add).toBeDisabled();
    expect(add).toHaveTextContent("需要主機的寫入權限");
    await userEvent.setup().click(screen.getByRole("button", { name: "hazel-tw-01 的更多操作" }));
    const item = await screen.findByRole("menuitem", { name: /改名與標籤/ });
    expect(item).toHaveAttribute("aria-disabled", "true");
  });
});

describe("add host", () => {
  function addCore() {
    const core = coreWith([tw]);
    let state = enrollment();
    core.on(`POST ${B}/node-enrollments`, () => ({
      status: 201,
      body: { enrollment: state, code: "HZX-7Q4M-2KD9-PX81-C3VB" },
    }));
    core.on(`GET ${B}/node-enrollments/enr-1`, () => ({ status: 200, body: { enrollment: state } }));
    return {
      core,
      set: (next: Partial<typeof state>) => {
        state = { ...state, ...next };
      },
    };
  }

  it("creates a one-time code, shows the command once and waits for the host", async () => {
    const { core } = addCore();
    const router = renderAt("/nodes/new");
    await h1("新增主機");
    const u = userEvent.setup();
    await u.type(screen.getByLabelText("顯示名稱"), "lab-pi-02");
    await u.type(screen.getByLabelText("標籤"), "lab, arm64");
    await u.click(screen.getByRole("button", { name: "產生註冊碼" }));
    const cmd = await screen.findByLabelText("註冊指令");
    expect(cmd).toHaveTextContent("--code HZX-7Q4M-2KD9-PX81-C3VB");
    expect(cmd.textContent).toMatch(/--core-url https:\/\/localhost:9801/);
    expect(screen.getByText("註冊碼只顯示這一次。離開或重新整理這一頁之後就看不到了。")).toBeInTheDocument();
    expect(screen.getByText(/^還有 \d+:\d\d 有效$|^即將失效$/)).toBeInTheDocument();
    expect(screen.getByText("等待主機使用這個註冊碼…")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?enrollment=enr-1");
    expect(core.callsTo(`POST ${B}/node-enrollments`)[0]!.body).toEqual({
      name: "lab-pi-02",
      tags: ["lab", "arm64"],
      group: "",
      ttlSeconds: 600,
    });
  });

  it("never shows the code again after the page is opened anew", async () => {
    addCore();
    renderAt("/nodes/new?enrollment=enr-1");
    expect(await screen.findByText(/註冊碼只在產生時顯示一次/)).toBeInTheDocument();
    expect(screen.queryByLabelText("註冊指令")).toBeNull();
    expect(document.body.textContent).not.toContain("HZX-7Q4M-2KD9");
    expect(screen.getByText("7Q4M")).toBeInTheDocument();
  });

  it("withdraws a code nobody used and offers a new one", async () => {
    const { core, set } = addCore();
    core.on(`DELETE ${B}/node-enrollments/enr-1`, () => {
      set({ state: "revoked", revokedAt: "2026-11-12T03:43:00Z" });
      return { status: 200, body: { enrollment: enrollment({ state: "revoked", revokedAt: "2026-11-12T03:43:00Z" }) } };
    });
    renderAt("/nodes/new?enrollment=enr-1");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "收回這個註冊碼" }));
    expect(await screen.findByText("註冊碼已收回")).toBeInTheDocument();
    await u.click(screen.getByRole("button", { name: "重新產生註冊碼" }));
    expect(await screen.findByRole("button", { name: "產生註冊碼" })).toBeInTheDocument();
  });

  it.each([
    ["expired", "註冊碼過期了，沒有主機使用"],
    ["rejected", "已拒絕這台主機"],
    ["approval_expired", "24 小時內沒有人核對，這次註冊已失效"],
  ])("says when the enrollment ended as %s", async (state, text) => {
    const { set } = addCore();
    set({ state });
    renderAt("/nodes/new?enrollment=enr-1");
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重新產生註冊碼" })).toBeInTheDocument();
  });

  it("shows what the host reported once it used the code, then approves it", async () => {
    const { core, set } = addCore();
    set({ state: "pending_approval", host: claimedHost });
    core.on(`POST ${B}/node-enrollments/enr-1/approve`, () => {
      set({ state: "approved", nodeId: "new-1", decidedAt: NOW });
      return { status: 200, body: { node: node({ id: "new-1", name: "lab-pi-02" }) } };
    });
    renderAt("/nodes/new?enrollment=enr-1");
    expect(await screen.findByText("lab-pi-02", { selector: "dd" })).toBeInTheDocument();
    for (const v of ["5c1e0000a09f", "Debian 13 · arm64", "0.1.0", "203.0.113.61", "SHA256:3f0b91de"])
      expect(screen.getByText(v, { selector: "dd" })).toBeInTheDocument();
    expect(screen.getByText("沒有相同金鑰或相同 machine-id 的既有主機。")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "是這台，加入" }));
    expect(await screen.findByText("lab-pi-02 已加入")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看這台主機" })).toHaveAttribute("href", "/nodes/new-1");
    expect(core.callsTo(`POST ${B}/node-enrollments/enr-1/approve`)[0]!.body).toEqual({});
  });

  it("requires choosing to replace the host with the same machine-id or to add a new one", async () => {
    const { core, set } = addCore();
    set({
      state: "pending_approval",
      host: claimedHost,
      matchingNodes: [
        { nodeId: "tw-01", name: "hazel-tw-01", state: "active", reason: "same_machine_id", keyRetired: false },
      ],
    });
    core.on(`POST ${B}/node-enrollments/enr-1/approve`, () => ({ status: 200, body: { node: tw } }));
    renderAt("/nodes/new?enrollment=enr-1");
    const approve = await screen.findByRole("button", { name: "是這台，加入" });
    expect(approve).toBeDisabled();
    expect(screen.getByText("同一個 machine-id")).toBeInTheDocument();
    const u = userEvent.setup();
    await u.click(screen.getByRole("radio", { name: /取代 hazel-tw-01/ }));
    expect(approve).toBeEnabled();
    await u.click(approve);
    await waitFor(() =>
      expect(core.callsTo(`POST ${B}/node-enrollments/enr-1/approve`)[0]!.body).toEqual({ replaceNodeId: "tw-01" }),
    );
  });

  it("sends asNewNode when the host is added as a new one", async () => {
    const { core, set } = addCore();
    set({
      state: "pending_approval",
      host: claimedHost,
      matchingNodes: [
        { nodeId: "tw-01", name: "hazel-tw-01", state: "active", reason: "same_machine_id", keyRetired: false },
      ],
    });
    core.on(`POST ${B}/node-enrollments/enr-1/approve`, () => ({ status: 200, body: { node: tw } }));
    renderAt("/nodes/new?enrollment=enr-1");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("radio", { name: /當成新主機/ }));
    await u.click(screen.getByRole("button", { name: "是這台，加入" }));
    await waitFor(() =>
      expect(core.callsTo(`POST ${B}/node-enrollments/enr-1/approve`)[0]!.body).toEqual({ asNewNode: true }),
    );
  });

  it("blocks approving a retired key and says the host needs a new one", async () => {
    const { set } = addCore();
    set({
      state: "pending_approval",
      host: claimedHost,
      matchingNodes: [
        {
          nodeId: "tw-01",
          name: "hazel-tw-01",
          state: "revoked",
          reason: "same_key",
          keyRetired: true,
          keyRetiredReason: "revoked",
        },
      ],
    });
    renderAt("/nodes/new?enrollment=enr-1");
    expect(await screen.findByRole("button", { name: "是這台，加入" })).toBeDisabled();
    expect(screen.getByText(/核准一定會被拒/)).toBeInTheDocument();
    expect(screen.getByText(/--new-identity/)).toBeInTheDocument();
  });

  it("rejects the host after a confirmation", async () => {
    const { core, set } = addCore();
    set({ state: "pending_approval", host: claimedHost });
    core.on(`POST ${B}/node-enrollments/enr-1/reject`, () => {
      set({ state: "rejected", decidedAt: NOW });
      return { status: 200, body: { enrollment: enrollment({ state: "rejected", decidedAt: NOW }) } };
    });
    renderAt("/nodes/new?enrollment=enr-1");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "不是，拒絕" }));
    const dialog = await screen.findByRole("dialog", { name: "拒絕 lab-pi-02" });
    expect(within(dialog).getByText("這個註冊碼已經用掉，不能再用。")).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "拒絕 lab-pi-02" }));
    expect(await screen.findByText("已拒絕這台主機")).toBeInTheDocument();
  });

  it("moves from waiting to the check by itself when the host uses the code", async () => {
    const { set } = addCore();
    renderAt("/nodes/new?enrollment=enr-1");
    await screen.findByText("等待主機使用這個註冊碼…");
    set({ state: "pending_approval", host: claimedHost });
    expect(await screen.findByText("SHA256:3f0b91de", {}, { timeout: 5000 })).toBeInTheDocument();
  });
});

describe("host actions", () => {
  async function openAction(name: string, actionName: string) {
    renderAt("/nodes");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: `${name} 的更多操作` }));
    await u.click(await screen.findByRole("menuitem", { name: actionName }));
    return { u, dialog: await screen.findByRole("dialog") };
  }

  it("renames and re-tags with the version read, and reloads after someone else changed the host", async () => {
    const core = coreWith();
    let first = true;
    core.on(`PATCH ${B}/nodes/tw-01`, () => {
      if (first) {
        first = false;
        return apiError(412, "VERSION_MISMATCH", { details: { currentVersion: 4 } });
      }
      return { status: 200, body: { node: { ...tw, name: "tw-main", version: 5 } } };
    });
    core.on(`GET ${B}/nodes/tw-01`, { status: 200, body: { node: { ...tw, group: "osaka", version: 4 } } });
    const { u, dialog } = await openAction("hazel-tw-01", "改名與標籤");
    const name = within(dialog).getByLabelText("顯示名稱");
    await u.clear(name);
    await u.type(name, "tw-main");
    await u.click(within(dialog).getByRole("button", { name: "儲存" }));
    expect(await within(dialog).findByText(/被別人改過/)).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "載入目前的值" }));
    await waitFor(() => expect(within(dialog).getByLabelText("群組")).toHaveValue("osaka"));
    await u.clear(within(dialog).getByLabelText("顯示名稱"));
    await u.type(within(dialog).getByLabelText("顯示名稱"), "tw-main");
    await u.click(within(dialog).getByRole("button", { name: "儲存" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const patches = core.callsTo(`PATCH ${B}/nodes/tw-01`);
    expect(patches.map((c) => c.body)).toEqual([{ name: "tw-main" }, { name: "tw-main" }]);
    const headers = (fetch as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls
      .filter((c) => String(c[0]).endsWith("/nodes/tw-01") && c[1]?.method === "PATCH")
      .map((c) => new Headers(c[1].headers).get("If-Match"));
    expect(headers).toEqual(['"v3"', '"v4"']);
  });

  it("asks before closing a dialog with unsaved input", async () => {
    coreWith();
    const { u, dialog } = await openAction("hazel-tw-01", "改名與標籤");
    await u.type(within(dialog).getByLabelText("群組"), "x");
    await u.keyboard("{Escape}");
    expect(await within(dialog).findByText("輸入的內容還沒儲存。")).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "不儲存，關閉" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("starts maintenance and lists the jobs that keep running", async () => {
    const core = coreWith();
    core.on(`PUT ${B}/nodes/tw-01/maintenance`, () => ({
      status: 200,
      body: {
        node: { ...tw, maintenance: { enabled: true, reason: "kernel", since: NOW } },
        affectedJobs: [{ id: "job-7", kind: "packages.upgrade", state: "running" }],
      },
    }));
    const { u, dialog } = await openAction("hazel-tw-01", "進入維護");
    expect(within(dialog).getByText("從現在起，這台主機的新工作會被擋下。")).toBeInTheDocument();
    await u.type(within(dialog).getByLabelText("原因"), "kernel");
    await u.click(within(dialog).getByRole("button", { name: "進入維護 hazel-tw-01" }));
    const done = await screen.findByRole("dialog", { name: "hazel-tw-01 已進入維護" });
    expect(within(done).getByRole("link", { name: "packages.upgrade" })).toHaveAttribute("href", "/jobs/job-7");
    expect(within(done).getByText("執行中")).toBeInTheDocument();
    expect(core.callsTo(`PUT ${B}/nodes/tw-01/maintenance`)[0]!.body).toEqual({ enabled: true, reason: "kernel" });
  });

  it("ends maintenance", async () => {
    const core = coreWith();
    core.on(`PUT ${B}/nodes/mini/maintenance`, {
      status: 200,
      body: { node: { ...mini, maintenance: { enabled: false } }, affectedJobs: [] },
    });
    const { u, dialog } = await openAction("lab-mini", "結束維護");
    expect(within(dialog).getByText(/接著自動重新檢查/)).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "結束維護 lab-mini" }));
    await waitFor(() =>
      expect(core.callsTo(`PUT ${B}/nodes/mini/maintenance`)[0]!.body).toEqual({ enabled: false, reason: "" }),
    );
  });

  it("rotates a certificate and says an offline host gets it later", async () => {
    const core = coreWith();
    core.on(`POST ${B}/nodes/jp-02/rotate-certificate`, { status: 200, body: { node: jp } });
    const { u, dialog } = await openAction("hazel-jp-02", "輪替憑證");
    expect(within(dialog).getByText("主機不在線，新憑證會在它下次連線時送出。")).toBeInTheDocument();
    expect(within(dialog).getByText("新憑證送達且主機用它連線之前，新舊兩張都有效。")).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "輪替 hazel-jp-02 的憑證" }));
    await waitFor(() => expect(core.callsTo(`POST ${B}/nodes/jp-02/rotate-certificate`)).toHaveLength(1));
  });

  it("revokes only after the host's name is typed, listing the consequences", async () => {
    const core = coreWith();
    core.on(`POST ${B}/nodes/jp-02/revoke`, { status: 200, body: { node: { ...jp, state: "revoked" } } });
    const { u, dialog } = await openAction("hazel-jp-02", "撤銷");
    for (const s of ["這台主機的連線立刻中斷。", "之後它再也連不回來。", "送給它的新工作一律被擋下。"])
      expect(within(dialog).getByText(s)).toBeInTheDocument();
    expect(within(dialog).getByText(/待送達/)).toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", { name: "撤銷 hazel-jp-02" });
    expect(confirm).toBeDisabled();
    await u.type(within(dialog).getByLabelText("輸入 hazel-jp-02 確認"), "hazel-jp-0");
    expect(confirm).toBeDisabled();
    await u.type(within(dialog).getByLabelText("輸入 hazel-jp-02 確認"), "2");
    await u.type(within(dialog).getByLabelText("原因"), "key leaked");
    await u.click(confirm);
    await waitFor(() =>
      expect(core.callsTo(`POST ${B}/nodes/jp-02/revoke`)[0]!.body).toEqual({ reason: "key leaked" }),
    );
  });

  it("keeps the four removal modes apart and only offers the one Core can do", async () => {
    const core = coreWith();
    core.on(`POST ${B}/nodes/tw-01/remove`, { status: 200, body: { node: { ...tw, state: "removed" } } });
    const { u, dialog } = await openAction("hazel-tw-01", "不再管理");
    expect(within(dialog).getByRole("radio", { name: /^不再管理/ })).toBeChecked();
    for (const [mode, ticket] of [
      ["移除 Agent", "037"],
      ["移除叢集節點", "043, 087"],
      ["刪除工作負載", "040, 044, 052"],
    ]) {
      const r = within(dialog).getByRole("radio", { name: new RegExp(`^${mode}`) });
      expect(r).toBeDisabled();
      expect(r).toHaveAccessibleDescription(`目前做不到：控制中心還沒有這項動作，由票 ${ticket} 提供。`);
    }
    expect(within(dialog).getByText(/主機本身、Agent 與上面的應用都不會被動到/)).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "不再管理 hazel-tw-01" }));
    await waitFor(() => expect(core.callsTo(`POST ${B}/nodes/tw-01/remove`)[0]!.body).toEqual({ mode: "unmanage" }));
  });
});

describe("host page", () => {
  it("shows the revocation still waiting to reach an offline host", async () => {
    const revoked = node({
      id: "jp-02",
      name: "hazel-jp-02",
      state: "revoked",
      connection: { state: "offline", lastContactAt: "2026-11-12T03:41:18Z" } as never,
      revocation: { requestedAt: NOW, reason: "key leaked", by: "u1", pending: true } as never,
      pendingSync: ["revocation"],
    });
    coreWith([revoked]);
    renderAt("/nodes/jp-02");
    await h1("hazel-jp-02");
    expect(screen.getAllByText("已撤銷 · 待送達").length).toBeGreaterThan(0);
    expect(screen.getAllByText("待送達").length).toBeGreaterThan(0);
    expect(screen.getByText("key leaked")).toBeInTheDocument();
  });

  it("shows the propagation delay once the revocation reached the host", async () => {
    const revoked = node({
      id: "jp-02",
      name: "hazel-jp-02",
      state: "revoked",
      revocation: {
        requestedAt: NOW,
        reason: "",
        by: "u1",
        pending: false,
        agentNotifiedAt: NOW,
        propagationSeconds: 90,
      } as never,
    });
    coreWith([revoked]);
    renderAt("/nodes/jp-02");
    expect(await screen.findByText("90 秒")).toBeInTheDocument();
  });

  it("shows a failed re-check after maintenance and runs it again", async () => {
    const failed = node({
      id: "tw-01",
      name: "hazel-tw-01",
      maintenance: {
        enabled: false,
        reason: "",
        endedAt: NOW,
        checkedAt: NOW,
        recheck: {
          state: "failed",
          requestedAt: NOW,
          finishedAt: NOW,
          identityMatches: false,
          detail: "agent answered for another node",
        },
      } as never,
    });
    const core = coreWith([failed]);
    const pending = {
      ...failed,
      maintenance: { ...failed.maintenance, recheck: { state: "pending", requestedAt: NOW, identityMatches: false } },
    };
    core.on(`POST ${B}/nodes/tw-01/recheck`, () => {
      core.on(`GET ${B}/nodes/tw-01`, { status: 200, body: { node: pending } });
      return { status: 200, body: { node: pending } };
    });
    renderAt("/nodes/tw-01");
    expect(await screen.findByText("失敗")).toBeInTheDocument();
    expect(screen.getByText("agent answered for another node")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "再檢查一次" }));
    expect(await screen.findByText("等待主機連線")).toBeInTheDocument();
  });

  it("shows a rotation that failed with its reason", async () => {
    const n = node({
      id: "tw-01",
      name: "hazel-tw-01",
      certificate: {
        serial: "1",
        notAfter: "2027-01-01T00:00:00Z",
        state: "valid",
        expiresInDays: 50,
        rotation: {
          newSerial: "2",
          newNotAfter: "2027-04-01T00:00:00Z",
          requestedAt: NOW,
          error: "agent refused the certificate",
        },
      } as never,
    });
    coreWith([n]);
    renderAt("/nodes/tw-01");
    expect(await screen.findByText("輪替失敗")).toBeInTheDocument();
    expect(screen.getByText(/agent refused the certificate/)).toBeInTheDocument();
  });

  it("says when the host does not exist", async () => {
    coreWith([]);
    renderAt("/nodes/nope/files");
    expect(await h1("找不到主機 nope")).toBeInTheDocument();
  });

  it("reads the host again every 10 seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const core = coreWith([tw]);
    renderAt("/nodes/tw-01");
    await h1("hazel-tw-01");
    const before = core.callsTo(`GET ${B}/nodes/tw-01`).length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_500);
    });
    expect(core.callsTo(`GET ${B}/nodes/tw-01`).length).toBeGreaterThan(before);
  });
});

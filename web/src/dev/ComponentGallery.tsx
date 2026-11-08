import { useTranslation } from "react-i18next";
import { ApiError } from "../api/client";
import { ComponentDownBanner } from "../components/ComponentDownBanner";
import { HostBar } from "../components/HostBar";
import { ApiErrorMessage, RawMessage } from "../components/RawMessage";
import { SaveBar } from "../components/SaveBar";
import { PageHead } from "../shell/PageShell";
import { PageSkeleton } from "../shell/PageStates";

/**
 * Development-only page (not in production builds): renders every shared component with sample
 * props so the Playwright contrast, target-size and name scans cover them. All values are samples.
 */
export function ComponentGallery() {
  const { t } = useTranslation();
  const noop = () => {};
  return (
    <>
      <PageHead title="Component gallery (dev)" />
      <HostBar
        hosts={[
          { id: "n2", name: "hazel-jp-02" },
          { id: "n3", name: "lab-mini" },
          { id: "n4", name: "edge-sg-01" },
          { id: "n5", name: "edge-sg-02" },
        ]}
        allHref="/nodes?status=unreachable"
        hostHref={(id) => `/nodes/${id}`}
      />
      <ComponentDownBanner
        name="Zentinel"
        kind="systemd"
        host="hazel-tw-01"
        stoppedAt="2026-11-08T03:30:00Z"
        now={new Date("2026-11-08T03:42:06Z")}
        exitCode={137}
        reason="OOMKilled"
        affected={[t("nav.websites"), t("nav.security")]}
        logTail={[
          "Nov 08 03:29:58 hazel-tw-01 zentinel[812]: upstream pool exhausted",
          "Nov 08 03:30:00 hazel-tw-01 systemd[1]: zentinel.service: Main process exited, code=killed, status=9/KILL",
        ]}
        canRestart
        failures={{
          count: 2,
          lastError: "Job for zentinel.service failed because the control process exited with error code.",
        }}
        onRestart={noop}
        onViewLogs={noop}
      />
      <ComponentDownBanner
        name="Ollama"
        kind="container"
        host="lab-mini"
        stoppedAt="2026-11-07T22:00:00Z"
        now={new Date("2026-11-08T03:42:06Z")}
        affected={[t("nav.ai")]}
        canRestart={false}
        noPermissionContact="tim"
        onRestart={noop}
        onViewLogs={noop}
      />
      <section className="hx-panel hx-panel--pad">
        <RawMessage
          summary="sshd 設定檢查沒有通過"
          raw={
            "/etc/ssh/sshd_config line 42: Bad configuration option: PermitRootLogn\n/etc/ssh/sshd_config: terminating, 1 bad configuration options"
          }
          source="sshd -t"
        />
      </section>
      <section className="hx-panel hx-panel--pad">
        <ApiErrorMessage
          error={
            new ApiError({
              status: 403,
              code: "FORBIDDEN",
              message: "missing platform:write on global scope",
              raw: '{"code":"FORBIDDEN","message":"missing platform:write on global scope","traceId":"7f3a9c2e"}',
              traceId: "7f3a9c2e",
            })
          }
        />
      </section>
      <ComponentDownBanner
        name="Zentinel"
        kind="systemd"
        host="hazel-tw-01"
        stoppedAt="2026-11-08T03:41:00Z"
        now={new Date("2026-11-08T03:42:06Z")}
        affected={[t("nav.websites")]}
        canRestart
        restarting
        onRestart={noop}
        onViewLogs={noop}
      />
      <PageSkeleton delayMs={0} />
      {/* Extra bars sit in their own boxes so the sticky bars do not stack; only the last one guards navigation. */}
      <div>
        <SaveBar
          changes={1}
          subject=".env"
          effect="儲存中"
          saveLabel="儲存"
          saving
          guardNavigation={false}
          onSave={noop}
          onDiscard={noop}
        />
      </div>
      <div>
        <SaveBar
          changes={1}
          subject=".env"
          saveLabel="儲存"
          error="磁碟唯讀（PLATFORM_DEGRADED）"
          guardNavigation={false}
          onSave={noop}
          onDiscard={noop}
        />
      </div>
      <SaveBar
        changes={3}
        effect="存檔後要重建 shop-api 才會生效"
        saveLabel="儲存並重建 shop-api"
        onSave={noop}
        onDiscard={noop}
      />
    </>
  );
}

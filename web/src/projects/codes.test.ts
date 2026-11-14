import { describe, expect, it } from "vite-plus/test";
import en from "../i18n/locales/en.json";
import zhTW from "../i18n/locales/zh-TW.json";

/*
 * Codes Core can send that the project screens show in the interface language. Core's OpenAPI types them as
 * plain strings, so they are listed here by hand from their sources; add a code here when Core adds one.
 *
 * FINDING_CODES: every `a.add(Severity…, "<code>", …)` in core/internal/projects/compose.go, plus the
 *   "target-" + state blockers of core/internal/projects/preview.go (Target* constants).
 * CHECK_CODES: the change-plan check ids and titles core/internal/composedeploy/deploy.go writes itself
 *   (the others are finding codes).
 * HOST_CHECK_CODES: the host check codes of agent/crates/privileged/src/handlers/compose.rs, which
 *   composedeploy writes into log and evidence lines ("host check <level> <CODE> …").
 * STEP_ERROR_CODES: the StepError codes of core/internal/composedeploy/deploy.go, the executor failure codes of
 *   core/internal/nodes, and the JOB_<state> codes core/internal/api/compose_deploy.go gives a deployment row.
 * COMPLETION_CHECKS: the jobs.Check names of composedeploy's Verify.
 */
const FINDING_CODES = [
  "build-context-missing",
  "build-context-remote",
  "cap-add",
  "compose-empty",
  "compose-invalid",
  "data-dir",
  "device",
  "dockerfile-missing",
  "dotenv-not-read",
  "engine-socket",
  "env-file-missing",
  "env-file-not-secret",
  "env-file-optional-missing",
  "extends-file-unsupported",
  "extension",
  "external-network",
  "external-volume",
  "file-missing",
  "home-path",
  "host-namespace",
  "host-path",
  "host-system-path",
  "include-unsupported",
  "mount-unsupported",
  "name-not-used",
  "not-applied",
  "object-from-environment",
  "path-outside-unit",
  "port-all-addresses",
  "privileged",
  "profile-disabled",
  "secret-file-not-secret",
  "security-opt",
  "variable-empty",
  "variable-missing",
  "yaml-invalid",
  "target-not-set",
  "target-not-probed",
  "target-not-found",
  "target-unsupported",
  "target-not-running",
  "target-no-compose",
];
const CHECK_CODES = [
  "target-node",
  "target-rootful",
  "target-ready",
  "target engine",
  "target node",
  "engine environment",
];
const HOST_CHECK_CODES = [
  "BIND_SOURCE_MISSING",
  "COMPOSE_INVALID",
  "DISK_LOW",
  "DISK_UNKNOWN",
  "ENGINE_UNREACHABLE",
  "HOST_RISK",
  "IMAGE_INVALID",
  "IMAGE_UNAVAILABLE",
  "IMAGE_UNCHECKED",
  "PORTS_UNKNOWN",
  "PORT_IN_USE",
  "PROJECT_NOT_PLATFORM_OWNED",
];
const STEP_ERROR_CODES = [
  "AGENT_RESULT_INVALID",
  "GRANT_FAILED",
  "HOST_CHECK_FAILED",
  "INVALID_PARAMS",
  "NODE_REQUIRED",
  "TARGET_MOVED",
  "TARGET_NOT_SET",
  "UNIT_CHANGED",
  "UNIT_GONE",
  "UNIT_TOO_LARGE_TO_SEND",
  "EXECUTOR_ACTION_FAILED",
  "EXECUTOR_PERMISSION_DENIED",
  "EXECUTOR_REFUSED",
  "EXECUTOR_UNREACHABLE",
  "JOB_FAILED",
  "JOB_CANCELLED",
  "JOB_ROLLED_BACK",
  "JOB_NEEDS_ATTENTION",
];
const COMPLETION_CHECKS = ["host-checks-passed", "deployment-recorded", "containers-running"];

const LOCALES = { "zh-TW": zhTW.projects.code, en: en.projects.code } as const;

describe.each(Object.entries(LOCALES))("%s has a string for every code Core sends", (_, code) => {
  const missing = (table: Record<string, string>, codes: string[]) => codes.filter((c) => !table[c]);
  it("Compose findings and blockers", () => expect(missing(code.finding, FINDING_CODES)).toEqual([]));
  it("plan checks", () => expect(missing(code.check, CHECK_CODES)).toEqual([]));
  it("host checks", () => expect(missing(code.hostCheck, HOST_CHECK_CODES)).toEqual([]));
  it("job and deployment errors", () => expect(missing(code.stepError, STEP_ERROR_CODES)).toEqual([]));
  it("completion checks", () => expect(missing(code.completion, COMPLETION_CHECKS)).toEqual([]));
  it("the steps of compose.check and compose.deploy", () => {
    expect(code.step.compose.check["0"]).toBeTruthy();
    expect(code.step.compose.deploy["0"]).toBeTruthy();
    expect(code.step.compose.deploy["1"]).toBeTruthy();
  });
});

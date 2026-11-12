import { Table } from "@cloudflare/kumo/components/table";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { StatusPill } from "../components/StatusPill";
import { ENROLLMENT, nodeKeys, nodesApi, type Enrollment } from "./api";
import { REFRESH_MS } from "./hooks";
import { ADD_NODE_PATH } from "./paths";
import { at } from "./status";

function useEnrollments(state: string) {
  return useQuery({
    queryKey: nodeKeys.enrollments(state),
    queryFn: ({ signal }) => nodesApi.enrollments(state, signal),
    refetchInterval: REFRESH_MS,
  });
}

/**
 * Codes still waiting for a host and hosts waiting to be checked, so a check is not lost when the admin left the
 * add-host page. Hidden when there are none, or when the account cannot read enrollments (nodes:read globally).
 */
export function Enrollments() {
  const { t } = useTranslation();
  const pending = useEnrollments(ENROLLMENT.pending);
  const waiting = useEnrollments(ENROLLMENT.waiting);
  const rows: Enrollment[] = [...(pending.data?.items ?? []), ...(waiting.data?.items ?? [])];
  if (rows.length === 0) return null;
  return (
    <section className="hx-panel" aria-labelledby="hx-enrollments-title">
      <div className="hx-panel-head">
        <h2 id="hx-enrollments-title">{t("nodes.enrollments.title", { count: rows.length })}</h2>
      </div>
      <div className="hx-table-wrap">
        <Table className="hx-table">
          <Table.Header>
            <Table.Row>
              <Table.Head>{t("nodes.enrollments.host")}</Table.Head>
              <Table.Head>{t("nodes.col.status")}</Table.Head>
              <Table.Head>{t("nodes.enrollments.deadline")}</Table.Head>
              <Table.Head>
                <span className="hx-visually-hidden">{t("nodes.col.actions")}</span>
              </Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {rows.map((e) => {
              const isPending = e.state === ENROLLMENT.pending;
              const name = e.host?.hostname || e.name || e.codeId;
              return (
                <Table.Row key={e.id}>
                  <Table.Cell>
                    <div className="hx-name">{name}</div>
                    <div className="hx-sub">{t("nodes.enrollments.codeId", { codeId: e.codeId })}</div>
                  </Table.Cell>
                  <Table.Cell>
                    <StatusPill tone={isPending ? "warn" : "info"}>
                      {isPending ? t("nodes.enrollments.pending") : t("nodes.enrollments.waiting")}
                    </StatusPill>
                  </Table.Cell>
                  <Table.Cell className="hx-nowrap">
                    {at(isPending ? e.host?.claimExpiresAt : e.expiresAt) ?? "—"}
                  </Table.Cell>
                  <Table.Cell className="hx-acts">
                    <Link
                      to={`${ADD_NODE_PATH}?enrollment=${encodeURIComponent(e.id)}`}
                      className="hx-btn-link"
                      aria-label={t(isPending ? "nodes.enrollments.checkNamed" : "nodes.enrollments.openNamed", {
                        name,
                      })}
                    >
                      {isPending ? t("nodes.enrollments.check") : t("nodes.enrollments.open")}
                    </Link>
                  </Table.Cell>
                </Table.Row>
              );
            })}
          </Table.Body>
        </Table>
      </div>
    </section>
  );
}

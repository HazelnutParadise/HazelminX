# 1Panel v2.4.1 OSS 功能基線

票 092 的交付物：每一條上游路由與畫面在 HazelminX 的對應位置與狀態。宣稱「功能已保留」前，在這裡登記證據。

## 檔案

| 檔案 | 內容 | 產生方式 |
|---|---|---|
| `1panel-v2.4.1-api-routes.json` | `core/router` 與 `agent/router` 的全部 HTTP 端點（793 條：core 81、agent 712），含檔案、群組、方法、路徑、handler | `scripts/baseline/extract_1panel_routes.py` |
| `1panel-v2.4.1-frontend-routes.json` | `frontend/src/routers` 的全部路由（108 條），含路徑、名稱、view 元件、是否隱藏 | 同上 |
| `1panel-v2.4.1-baseline.csv` | 上述兩者合併的對照表，後四欄由人工填寫 | 同上；填寫後不要重新產生覆蓋，改用合併 |

重新產生：

```bash
python3 scripts/baseline/extract_1panel_routes.py <1Panel v2.4.1 checkout> docs/baseline
```

## 對照表欄位

| 欄位 | 填法 |
|---|---|
| `hazelminx_ticket` | 擁有這項功能的票號，例如 `040`；多張票以 `;` 分隔 |
| `hazelminx_status` | `preserved`（保留）、`replaced`（替代，附替代方式）、`extended`（擴充）、`removed-approved`（經批准移除，附批准紀錄）、`pending`（尚未處理） |
| `evidence` | 測試編號、PR 或畫面路徑；沒有證據就留空，不填「完成」 |

## 規則

- 商業版（xpack／Pro／Enterprise）受控的端點與畫面不算 OSS 基線；登記為 `removed-approved` 並註明「商業版」。前端判定方式見 `docs/research/1panel-frontend.md` 第 4 節，後端 stub 位置見 `docs/research/1panel-backend.md` Q11。
- 路由數量只是入口。同一端點背後的匯入、匯出、例外與設定檔行為也要對照（PAR-001），對照方式記在該票。
- 上游安全修補追蹤表（PAR-007）另建 `security-patches.md`，每次發布附已合併／替代／不適用的紀錄。

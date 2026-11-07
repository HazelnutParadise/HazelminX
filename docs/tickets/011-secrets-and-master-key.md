# 011 — 管理員可以安全保存機密，且主金鑰可輪替並驗證舊備份仍可解密

**Epic:** 基礎建設（`epic:foundation`）  
**User Story:** 使用者可以安全執行長時間操作  
**Milestone:** M0 基礎與設計  
**Blocked by:** [003 管理員可以啟動 hazelminx-core 並通過健康檢查](003-core-skeleton.md)  
**Status:** done  
**規格章節:** 35｜平台資料儲存、機密與復原包  
**Issue:** [#11](https://github.com/HazelnutParadise/hazelminx/issues/11)  

## What it delivers

機密參照模型（類型、加密值、版本、使用範圍、輪替、到期）、主金鑰與資料庫分離保存、雜湊優先的 Token 查驗、輪替流程、斷電測試、復原金鑰的「已取得」檢查。

## Acceptance criteria

- [x] **DAT-001** 管理員可以在單機模式不安裝額外資料庫容器就啟動平台。 — 完成條件：關閉所有容器後，登入、節點與工作記錄仍可使用。
  證據：平台資料是本機 `core.db` 與 `master.key` 兩個檔案，執行檔無任何容器或網路依賴（`core/go.mod` 只有 Gin 與純 Go SQLite）；`TestRecordsSurviveRestart`（`core/internal/store/durability_test.go`）與 `TestPlatformDataRecoveryStates`（`core/cmd/hazelminx-core/main_test.go`）證明重啟後記錄仍在。登入、節點、工作本身的資料表由票 009、004、008 建立，不在本票。
- [x] **DAT-002** 系統可以在中斷寫入後恢復管理資料。 — 完成條件：斷電測試不破壞已確認的交易；損壞時拒絕冒用空白新資料庫。
  證據：`TestCommittedRowsSurviveProcessKill`（子程序 SIGKILL 後所有已回報 commit 的列都在）、`TestUncommittedWriteLeavesNoTraceInDiskSnapshot`、`TestSynchronousFull`；損壞：`TestCorruptHeaderIsRefusedAndLeftUntouched`、`TestCorruptPageIsRefused`、`TestOpenExistingRefusesMissingFile`、`TestPlatformDataRecoveryStates`。實機：`hazelminx-core` 啟動後 `kill -9` 再啟動 `/healthz` 正常；覆寫檔頭後啟動回 `refusing to start, recovery needed: store: database is damaged`，exit 1，檔案原樣。
- [x] **DAT-004** 管理員可以輪替機密資料。 — 完成條件：使用範圍、失敗回復與舊備份解密有測試；不把舊值放進變更紀錄。
  證據（`core/internal/secrets/service_test.go`）：使用範圍 `TestCreateAndRevealWithinScope`、`TestScopeChangeAndDelete`；失敗回復 `TestMasterKeyRotationFailureRollsBack`、`TestInterruptedRotationBeforeCommitIsDropped`、`TestInterruptedRotationAfterCommitIsCompleted`、`TestVerifyAllReportsDamageByLocationOnly`；舊備份解密 `TestOldBackupDecryptsAfterRotation`（輪替前的資料庫副本以輪替後的主金鑰檔開啟可解密；缺少舊世代的主金鑰檔則開啟即拒絕並點名世代）；舊值不進變更紀錄 `TestChangeRecordNeverContainsValues`（變更列與整個資料庫檔案位元組都不含新舊明文）。
- [x] 關閉所有容器後登入、節點與工作記錄仍可使用 — 同 DAT-001；本票負責資料層，三種記錄的資料表由各自的票接上。
- [x] 損壞時拒絕冒用空白新資料庫 — 同 DAT-002。

## Related requirements（由其他票擁有）

- DAT-003（擁有票 078）：管理員可以備份平台設定與秘密。

## Delivery record

- 需求 ID：DAT-001、DAT-002、DAT-004（T-012 的 Core 側行為）。
- 變更：`core/internal/secrets/`（新套件：鑰匙圈、AES-256-GCM 封裝、服務、Token 雜湊）、`core/internal/store/migrations/0004_secrets.sql`、`store.go`（`quick_check`、`ErrCorrupt`／`ErrMissing`／`OpenExisting`、`synchronous=FULL`、先以 0600 建檔）、`config.MasterKeyPath()`、`main.go` 的 `openPlatformData`。
- 測試環境：macOS arm64、Go 1.26.5、`go vet ./...` 與 `go test -race ./...` 全綠；store 與 secrets 套件 `-count=3` 無抖動。Linux CI（票 006）尚未存在。
- 正常案例：建立→指定消費者讀取→輪替值→固定舊版本仍可讀；主金鑰輪替後全部可讀且新值用新世代；復原材料確認紀錄重啟後仍在。
- 失敗案例：範圍外消費者、過期、換列的密文、重新包裝途中失敗、兩種斷點的中斷輪替、主金鑰檔權限過寬或損壞、資料庫損壞或遺失、主金鑰檔缺少資料引用的世代。
- 資料與安全影響：新增四張表；資料庫與 WAL 檔改為一律 0600；每筆 commit 改為 fsync（`synchronous=FULL`），管理資料量小可接受；主金鑰只在 `master.key`，日誌只印世代 ID。
- 未驗證：真實斷電（只做了程序層 SIGKILL 與磁碟快照）；Linux 檔案系統；復原包匯出（票 078）只留 `RecoveryMaterialSource` 介面；登入／節點／工作表（票 009／004／008）接上後的 DAT-001 端到端。

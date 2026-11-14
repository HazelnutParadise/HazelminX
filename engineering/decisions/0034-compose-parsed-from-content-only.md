# 0034 — Core 解析 Compose 時只用上傳的內容，不讀自己主機的檔案

**Status:** accepted
**Date:** 2026-11-14

## Context
票 044.1 要用 Docker Compose 自己用的解析器 compose-go 檢查使用者匯入的 Compose 檔，結果才會跟原生 `docker compose` 一致（T-052）。compose-go 在幾個地方會自己去讀磁碟：`include:`、`extends.file`、`env_file`、`label_file`，以及 include 目錄裡的 `.env`。Core 以平台帳號執行，資料目錄裡有資料庫與主金鑰；使用者寫一行 `include: [/var/lib/hazelminx/master.key]` 或 `env_file: ${X:-../../etc/passwd}`，解析器就會替他讀 Core 主機上的檔案，錯誤訊息還可能把內容帶出來。

## Decision
- 檔案以內容交給 compose-go，工作目錄是一個不存在的根目錄 `/hazelminx-unit`，回答裡的路徑都換回相對於單元的路徑。
- 解析前先讀一次原始 YAML 的每一份文件（`---` 分隔的每一段都讀）：有頂層 `include:` 或服務的 `extends.file` 就拒絕並寫出原因。這一步只負責給清楚的原因；真正擋下讀檔的是解析器裡的載入器：Core 放一個接受所有引用、一律拒絕的載入器，排在 compose-go 自己的磁碟載入器前面，原始檢查漏掉的寫法也讀不到檔案。include 由票 110 另外做，做法是把被帶入的檔案也當成單元的檔案。
- 關掉環境變數檔與標籤檔的解析（`SkipResolveEnvironment`、`SkipResolveLabels`）；除了那個一律拒絕的載入器，不設其他載入器。這些路徑只檢查是否在單元裡、是否是上傳的檔案。
- 替換變數用空的環境：Core 自己的環境變數不會滲進來，`.env` 不讀。變數的值由票 045 的分層提供。
- 路徑解析不用 compose-go 的 `ResolvePaths`，改由 Core 自己以專案目錄解析，才分得出「相對路徑跑出單元」與「本來就是主機上的絕對路徑」。T-052 的比對測試證明結果與原生 Compose 一致。

## Options rejected
- **把檔案寫進暫存目錄再讓 compose-go 照常讀**：`../` 與絕對路徑一樣讀得到 Core 主機的檔案，暫存目錄擋不住。
- **在 Agent 上跑 `docker compose config` 檢查**：結果最準，但每次編輯都要主機在線、要經高權限執行器，編輯器也沒辦法離線檢查。主機上的檢查留給部署前的預檢（票 044.2）。
- **自己寫 Compose 解析**：合併、替換、profile 的細節多，自己寫一定跟原生不一致，違反 15.2「交由相容解析器處理」。

## Consequences
- `include:` 與跨檔 `extends` 暫時不能用，使用者要把檔案列進合併順序；票 110 補上時要沿用「只讀單元內檔案」的限制。
- 只靠自己預先掃 YAML 擋讀檔不夠：審查時發現第二份 YAML 文件裡的 `extends.file` 繞過了只讀第一份的掃描，所以讀檔一定要在載入器裡擋。
- 升級 compose-go 時要重看它新增的讀檔位置（`os.ReadFile`、`os.Open`、`os.Stat`），並重跑 `TestNeverReadsTheDisk` 與 T-052 的比對測試。
- 影響的票：044.1、044.2、110、049（Git 來源的檔案一樣以內容交給解析器）。

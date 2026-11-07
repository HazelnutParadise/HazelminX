# 111 — 管理員可以建立多個模型庫、在面板搜尋並下載 Hugging Face 模型，再用內建的 vLLM 或 llama.cpp 部署推論服務

**Epic:** AI 與模型（`epic:ai`）  
**User Story:** 不用自己裝推論引擎、不用進主機打指令，就能把 Hugging Face 上的模型放到指定磁碟並跑起來  
**Milestone:** M4 資料庫、AI、備份與監控  
**Blocked by:** [074](074-ollama-models.md), [075](075-gpu-chat-inference-api.md)  
**Status:** blocked  
**規格章節:** 增補 05｜53｜內建 Hugging Face 模型 runtime 與模型庫  
**Issue:** [#118](https://github.com/HazelnutParadise/hazelminx/issues/118)  

## What it delivers

模型庫：一台主機可以有多個，每顆資料磁碟有預設位置 `<掛載點>/hazelminx/models`，第一次用才建立，也可以用資料夾選擇器自選；綁磁碟 UUID，有配額，可以搬移。搜尋 Hugging Face Hub（篩選格式、任務、參數量、授權），結果顯示檔案大小、適用 runtime、放不放得下。下載是可續傳、核對 sha256 的工作；需要同意條款的模型用機密庫裡的 Hugging Face Token。內建 vLLM（NVIDIA GPU、safetensors）與 llama.cpp（GGUF、CPU 也能跑）兩種 runtime，以平台管理的容器執行，模型唯讀掛載，對外走 API 代理。不內建已進入維護模式的 TGI。

## Acceptance criteria

- [ ] **HFM-001**（增補 05）管理員可以在主機上建立多個模型庫，指定磁碟與路徑。 — 完成條件：每顆資料磁碟有預設位置，第一次使用才建立；可以用資料夾選擇器選其他路徑；模型庫綁磁碟 UUID，磁碟不在時不寫入根目錄；可以設配額。
- [ ] **HFM-002**（增補 05）使用者可以在面板搜尋 Hugging Face 模型。 — 完成條件：依 53.3 篩選與排序；結果顯示檔案大小、適用 runtime、放不放得下與是否需要同意條款；Hub 連不到時改為直接輸入名稱，不顯示假清單。
- [ ] **HFM-003**（增補 05）使用者可以把模型下載到指定的模型庫。 — 完成條件：是一個工作，有進度、可取消、可續傳；每個檔案核對 sha256；配額或空間不夠時開始前擋下；GGUF 只下載選定的量化檔；記下 commit。
- [ ] **HFM-004**（增補 05）管理員可以設定 Hugging Face Token。 — 完成條件：存在機密庫；只用於下載；不進入 runtime 容器；稽核只記錄使用，不記錄值。
- [ ] **HFM-005**（增補 05）管理員可以用內建的 vLLM 或 llama.cpp 部署推論服務。 — 完成條件：只列出適用該模型格式與硬體的 runtime；通過 GPU 四段檢查才啟動；模型唯讀掛載；預設只綁本機，對外走 API 代理。
- [ ] **HFM-006**（增補 05）使用者可以看到推論服務的狀態並直接測試。 — 完成條件：顯示載入進度、GPU 記憶體、tok/s 與首字延遲；可以在 AI 頁測試對話。
- [ ] **HFM-007**（增補 05）管理員可以搬移或刪除模型庫，以及刪除模型。 — 完成條件：搬移先複製並核對再切換；有服務在用的模型與模型庫不能刪，並列出使用中的服務。
- [ ] **HFM-008**（增補 05）使用者可以看到 Hub 上的新版本並自己決定是否更新。 — 完成條件：下載時記錄 commit；有新版時標示；不自動更新。

## Acceptance tests

- **T-221** tw-01 有兩顆資料磁碟 `/mnt/data` 與 `/mnt/fast`，第一次在 `/mnt/fast` 建立模型庫。 → 建立 `/mnt/fast/hazelminx/models`；`/mnt/data` 還沒用過，沒有建立資料夾；模型庫顯示綁定的磁碟 UUID。
- **T-222** 把 `/mnt/fast` 卸載後重新開機。 → 模型庫標「磁碟不在」；用到它的推論服務沒有啟動；根目錄沒有多出 `hazelminx/models`。
- **T-223** 搜尋 `qwen2.5 7b`，格式選 GGUF，主機選 lab-mini（沒有 GPU，記憶體 16 GiB）。 → 結果列出各量化檔與大小；Q4_K_M 標「放得下 · CPU」；vLLM 不在可用 runtime 裡，並寫出「需要 NVIDIA GPU」。
- **T-224** 下載一個需要同意條款的模型，平台沒有設定 Hugging Face Token。 → 下載前擋下並說明要設定 Token 與到 Hub 同意條款；設定後下載成功；runtime 容器的環境變數裡沒有這組 Token。
- **T-225** 下載 9 GiB 的模型時中斷網路 2 分鐘。 → 工作暫停並顯示原因；網路恢復後從中斷處續傳；完成後 sha256 全部相符才標成可用。
- **T-226** 模型庫配額剩 5 GiB，要下載 9 GiB 的模型。 → 開始前擋下並寫出差多少；沒有下載任何部分檔案。
- **T-227** 用 llama.cpp 在 lab-mini 部署 GGUF 模型，再用 vLLM 在 tw-01（L4 GPU）部署 safetensors 模型。 → 兩個推論服務都啟動；模型目錄在容器內是唯讀；兩者都只綁本機；從 API 代理可以呼叫；AI 頁顯示 tok/s 與首字延遲。
- **T-228** 刪除一個正被推論服務使用的模型。 → 擋下並列出使用中的服務；停止並刪除該服務後才能刪除模型。
- **T-229** 把模型庫從 `/mnt/data` 搬到 `/mnt/fast`，搬移期間推論服務持續接請求。 → 複製並核對 sha256 後才切換；切換後服務重新載入新位置；舊位置在切換成功後才刪除。
- **T-230** Hub 連不到。 → 搜尋顯示「連不到 Hugging Face」與原因；提供直接輸入模型名稱；不顯示快取或假造的清單。

## Delivery record

需求 ID → 變更 → 測試環境 → 正常與失敗案例 → 證據 → 未驗證部分，依規格第 46.4 節填寫於 PR。

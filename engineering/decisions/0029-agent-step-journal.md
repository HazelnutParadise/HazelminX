# 0029 — Agent 每個步驟前後先寫 journal 才回報

**Status:** superseded by 0032
**Date:** 2026-11-08

## Context
ENG-003 不把執行中工作重設成功或重跑；可重試步驟若一律拒絕，對帳後的重跑會永遠卡住。

## Decision
Agent 每個步驟開始前與結束後先寫 append-only `journal.jsonl` 並 fsync 才回報；同一工作步驟有完成或失敗紀錄就重送紀錄不重跑；重開機時中斷的步驟只有 Core 對帳後送來、且標記可重試時才再跑。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：007、008。

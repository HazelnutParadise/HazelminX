# 0024 — 管理 API 的列表格式與錯誤碼統一

**Status:** accepted
**Date:** 2026-11-08

## Context
使用者決定（2026-11-08）；尚未發版，統一後前端只需處理一套格式。

## Decision
`/management/v1` 列表統一回 `{items, page}`、以 keyset 游標翻頁；`INVALID_REQUEST`、`JOB_KIND_UNKNOWN` 併入 `VALIDATION_FAILED`，`JOB_NOT_FOUND` 併入 `NOT_FOUND`，`CONFLICT` 改名 `ALREADY_EXISTS`。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：010、005、017。

# 0025 — 版本過期回 412，缺少版本回 428

**Status:** accepted
**Date:** 2026-11-08

## Context
使用者決定（2026-11-08）；RFC 9110 13.1.1。

## Decision
版本過期回 `412 VERSION_MISMATCH`，缺少或 `*` 回 `428 VERSION_REQUIRED`；`409` 只用於資源狀態擋下。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：010、015、016。

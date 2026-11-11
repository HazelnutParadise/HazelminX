# 0023 — 主金鑰存在資料目錄的 master.key，多世代保留

**Status:** accepted
**Date:** 2026-11-07

## Context
35.2 主金鑰不與資料庫同放、輪替後舊備份須可解密；DAT-002 已確認交易不可丟；T-012 不冒用空白庫。

## Decision
主金鑰存 `<data-dir>/master.key`（多世代、舊世代永不刪）；SQLite 改 `synchronous=FULL`；`master.key` 存在而 `core.db` 不在視為遺失而非全新安裝。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：011、078、013。

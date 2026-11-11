# 0018 — Core 的帳本副本走專用訊息與短效授權憑證

**Status:** accepted
**Date:** 2026-11-08

## Context
步驟結果上限 256 KiB，而且每次同步都會留一筆工作紀錄；執行器只接受 Core 簽的憑證。

## Decision
Core 的帳本副本用新訊息 `ledger_pull`／`ledger_page`／`ledger_unavailable`（能力 `host.ledger`），不走工作與步驟；Core 為每一頁簽一張只有 `host.ledger.read`、5 分鐘的授權憑證；簽發金鑰在機密服務，公鑰寫在資料目錄的 `privileged-issuer.pub`。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：113、013、019。

# 0017 — 主機變更帳本由高權限執行器寫

**Status:** accepted
**Date:** 2026-11-08

## Context
增補 07 第 55.1 節要求沒寫帳本的變更一律拒絕、主機連不上 Core 時仍能只靠本機那份移除。

## Decision
主機變更帳本由高權限執行器寫：每個動作宣告會改的對象，先同步寫意圖行才執行；帳本在 `/var/lib/hazelminx/ledger`（root 0700，上層不得可被他人寫入），每行帶前一行的 SHA-256；Core 拿原始行驗證後保存副本。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：113、013、037、007。

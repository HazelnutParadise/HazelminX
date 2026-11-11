# 0022 — Passkey 以請求主機名稱為 RP ID

**Status:** accepted
**Date:** 2026-11-08

## Context
WebAuthn RP ID 規則；T-130 要求變更前說明影響。

## Decision
Passkey 以請求主機名稱為 RP ID，只認完全相同的網域；網域變更前用 `auth/passkeys/domain-impact` 檢查。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：014、018。

# 0020 — 高權限角色沒有第二驗證時限制工作階段

**Status:** accepted
**Date:** 2026-11-08

## Context
IAM-002 高權限帳號的安全預設；主機維運者有終端機與 host:write，等同 root。

## Decision
擁有者、平台管理員、主機維運者沒有第二驗證時，工作階段只能走綁定白名單，也不能移除最後一個第二驗證。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：014、015、027。

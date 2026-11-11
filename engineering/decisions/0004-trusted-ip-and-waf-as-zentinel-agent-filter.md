# 0004 — 可信 IP 與 WAF 用 Zentinel agent filter 實作

**Status:** accepted
**Date:** 2026-11-06

## Context
Zentinel 研究：核心 WAF 未接線、只有 X-Forwarded-For 處理。

## Decision
可信 IP 與 WAF 以 Zentinel agent filter 實作；核心 `waf.mode` 不用。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：064、065。

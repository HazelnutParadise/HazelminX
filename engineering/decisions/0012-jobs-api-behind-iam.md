# 0012 — 工作 API 掛在 IAM 驗證之後

**Status:** accepted
**Date:** 2026-11-07

## Context
合併 008 與 009 時工作 API 原本不需登入；AGENTS.md 要求危險操作在後端驗權。

## Decision
`/management/v1/jobs` 掛在 IAM 驗證之後：讀取要全域 `platform:read`，提交與取消要全域 `platform:write`；各工作種類的權限與範圍由票 010 定。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：008、009、010。

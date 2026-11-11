# 0015 — API 型別產生器放在獨立小套件

**Status:** accepted
**Date:** 2026-11-08

## Context
openapi-typescript 需要 TypeScript 5 的編譯器 API，TypeScript 7 沒有。

## Decision
API 型別產生器放在 `web/tools/openapi/` 的獨立小套件。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：005、010。

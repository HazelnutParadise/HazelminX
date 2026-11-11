# 0013 — Kumo 主要與危險按鈕用 !important 改成實心 token

**Status:** accepted
**Date:** 2026-11-08

## Context
Kumo 以 inline style 把 brand 調亮 30% 並鎖白字，深色榛果底對比只有 2.00:1；覆蓋 token 無效。

## Decision
Kumo 主要／危險按鈕在 `@layer base` 用 `!important` 改成實心 token＋hazel-ink 字。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：005。

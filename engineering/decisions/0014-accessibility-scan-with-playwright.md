# 0014 — 對比、觸控目標、可及名稱用 Playwright 掃描

**Status:** accepted
**Date:** 2026-11-08

## Context
`design/tools/` 只有版面掃描；axe 遇到偽元素與兄弟層的底色只回 incomplete。

## Decision
對比、觸控目標、可及名稱改用 Playwright 掃描（`web/e2e/scan.ts`），另自算文字對比。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：005、096。

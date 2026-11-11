# 0019 — 有第二驗證的帳號分兩步登入

**Status:** accepted
**Date:** 2026-11-08

## Context
使用者核准的介面變更（2026-11-08），對照設計稿 Login／Setup 兩步流程。

## Decision
有第二驗證的帳號，密碼正確後只拿到一次性挑戰，第二步通過才開工作階段；`setup` 建好擁有者後直接登入。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：014、005。

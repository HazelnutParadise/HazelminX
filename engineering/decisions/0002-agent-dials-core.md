# 0002 — Agent 主動連到 Core

**Status:** accepted
**Date:** 2026-11-06

## Context
NOD-002 不要求節點開公網管理 Port。

## Decision
Agent 主動連 Core（mTLS WebSocket），不沿用 Periphery 的 Core 連 Agent 模式。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：004、007、019。

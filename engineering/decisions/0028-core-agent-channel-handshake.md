# 0028 — Core↔Agent 通道的握手與訊息驗證

**Status:** accepted
**Date:** 2026-11-08

## Context
NOD-002 不開節點入站埠；T-024 經終止 TLS 的代理仍要有獨立節點身分，而且代理不能在握手後插入或改寫訊息（規格 32 章「之後每次收命令也要確認」）；Komodo 的 Noise 握手在 Go 端沒有相容實作。

## Decision
Core↔Agent 通道：Agent 撥出到 Core 的 TLS 1.3 WebSocket（預設 `:9801`）；Core 有自己的 Ed25519 CA，私鑰存機密服務；雙方在握手時各自簽雙方 nonce、雙方 X25519 公鑰、協定版本、節點指紋與 session ID；之後每則訊息帶方向序號與 HMAC；直連時另要求 TLS 用戶端憑證與握手身分一致；HTTP Header 一律不當身分。

## Options rejected
原本的決策紀錄沒有列出其他選項。

## Consequences
影響的票：007、019。

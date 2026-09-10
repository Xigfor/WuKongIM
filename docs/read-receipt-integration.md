# Personal chat read receipts

Source: user-relayed W request, 2026-09-09; Flutter `origin/dev` fbf508d / f67a474.
Confirmed scope: personal chat (`channel_type=1`), the sender can see that the peer has read messages. Group receipts are outside this request.

1. Input: `POST /message/receipt`, JSON `{ "uid":"42", "channel_id":"99", "channel_type":1, "message_seqs":[101,102] }`, `Authorization: Bearer <mall login token>`.
2. Authority: the fixed HTTPS mall `/sso/info` endpoint validates the Bearer session and its returned member ID must exactly match `uid`; IM/push device tokens are not accepted as an alternative; read only existing messages sent by `channel_id` to this user with the receipt setting enabled. An invalid batch writes nothing.
3. Success: HTTP 200 `{ "status":200 }`; invalid/auth/ownership/not-found/unavailable responses are HTTP 400/401/403/404/503, never success-shaped errors.
4. Storage: additive Pebble namespace `0x1801`, atomic synced batch, slot-log replication; repeated receipt/log replay preserves `readed_count=1` and the original version. No message content is copied.
5. Online refresh: durable `sync_once=1`, `red_dot=0` type-99 `messageReaded` command addressed to the sender; `param.channel_id` is the reader (sender's peer).
6. Recovery: authenticated `POST /message/extra/sync` with the same pair, `extra_version` and `limit` (default 100, max 200) returns an ascending JSON array with `message_id_str`, `readed`, `readed_count`, `extra_version`. IDs stay strings. Cursor follows receipt commit order, including old messages read later.
7. Flutter wiring: preserve existing single/double checks and navigation; attach login token; batch reports by 200; save all sync pages through WKIM; sync on entering/reconnecting/resuming and loading history. Missing token or HTTP failure does not acknowledge local read reporting.
8. Validation: HTTP contract/ownership/atomic failure tests, durable storage/reopen/concurrent retry tests, replicated command apply/replay test, Flutter transport/pagination and existing read-status tests. Optional full local node test: `go test -tags receipt_integration ./internal/server -run '^TestMessageReceiptRoundTrip$' -count=1 -timeout=75s`.

The optional full node test is currently blocked before receipt handling: an isolated node's slot leader initialization does not become usable for `/user/token`. This is not evidence of a passed TCP round trip. Public authenticated receipt/command sync and two-device UI acceptance must be recorded separately.

## 2026-09-10 authentication correction

The App sends its mall login token. The original IM-device comparison rejected
valid mall sessions (confirmed with mall `/sso/info` 200 and both IM routes 401).
Both routes now use mall authentication with a 5-second timeout, no credential
redirects, exact integer/string member ID comparison, and fail-closed 503 on
upstream errors. Invalid/expired sessions or mismatched uid remain 401. No JWT
payload is trusted without mall validation; no IM device-token fallback is used.
`TestMessageReceiptMallLoginBoundary` exercises both routes through the real
authorizer and HTTP transport boundary, and runs in the existing CI release gate.
The optional isolated TCP test stubs only the mall authority; it is not evidence
of acceptance with a real mall session. Production verification must use the
existing mall login helper. The iOS Bearer request contract is unchanged.

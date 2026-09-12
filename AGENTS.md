# AGENTS.md — PiCam Bot

Coding conventions and guidance for AI agents and contributors working on this repo.

## Repository layout

```
picambot/
├── main.go            # Entry point: wires dependencies, starts bot
├── config.go          # LoadConfig() — env vars → Config struct
├── internal/
│   ├── bot/           # Telegram polling, command dispatch, auth
│   ├── state/         # FSM (4 states) + file-backed StateStore
│   ├── motion/        # MotionClient interface + HTTP + systemctl wrapper
│   ├── watcher/       # EventWatcher (fsnotify) for motion output dir
│   └── notify/        # Sender — text/photo/video to Telegram
└── docs/
    └── picambot-spec.md
```

## Package responsibilities (do not cross these boundaries)

- `bot` knows about `state`, `motion`, `watcher`, `notify`. It is the only package that calls Telegram send APIs.
- `motion` knows nothing about Telegram or state.
- `watcher` knows nothing about Telegram, state, or motion.
- `notify` knows only about `tgbotapi`. It does not touch state or motion.
- `state` has zero external dependencies (stdlib only).

## Key design decisions

- **All I/O is behind interfaces** (`MotionClient`, `StateStore`, `EventWatcher`). Never accept concrete types in function signatures unless it's package-internal.
- **State file is written atomically** via `.tmp` + `os.Rename`. Never write state.json directly.
- **Auth checks `From.ID`**, not `Chat.ID`. This is intentional — works in DMs and groups.
- **`/record N` runs in a goroutine** with a context cancel stored in `Bot.recordCancel`. `/stop` cancels it.
- **ChatID is persisted** in state.json so motion alerts work immediately after restart.

## Testing

Run unit tests:
```sh
go test -race ./...
```

Run integration tests (no external services required — all fakes):
```sh
go test -race -tags integration ./...
```

Tests use:
- `t.TempDir()` for all file I/O (never write to real paths)
- `httptest.NewServer` for the motion webcontrol API
- `fakeMotionClient` struct implementing `motion.MotionClient` for bot-level tests
- Real `PersistentStateStore` with `t.TempDir()` (file I/O is cheap and tests the real behaviour)

## State machine

```
sleep ──/start──► monitoring ──/detect──► detecting
                      │                      │
                   /record               /stopdetect
                      │                      │
                   recording ──────────► monitoring
                      │
                   /stop (any state) ──► sleep
```

Invalid transitions return `state.ErrInvalidTransition`. `/stop` from `sleep` is invalid.

## Environment variables

| Variable | Required | Default |
|---|---|---|
| `TELEGRAM_TOKEN` | Yes | — |
| `ALLOWED_USER_IDS` | Yes | — |
| `MOTION_HOST` | No | `127.0.0.1` |
| `MOTION_PORT` | No | `7999` |
| `MOTION_TARGET_DIR` | No | `/var/lib/motion` |
| `STATE_FILE` | No | `/var/lib/picambot/state.json` |
| `LOG_LEVEL` | No | `info` |

## Build & deploy

```sh
# Build for Raspberry Pi 4 (arm64)
GOOS=linux GOARCH=arm64 go build -o picambot .

# Copy to Pi
scp picambot pi@raspberrypi:/usr/local/bin/picambot
```

Systemd unit, sudoers rule, and motion.conf are in `docs/picambot-spec.md §7`.

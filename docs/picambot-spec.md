# PiCam Bot — Technical Specification

**Raspberry Pi 4 · Telegram Camera Control System**  
v1.0 · March 2026

---

## Table of Contents

1. [Overview](#1-overview)
2. [Goals & Non-Goals](#2-goals--non-goals)
3. [User Stories](#3-user-stories)
4. [Functional Requirements](#4-functional-requirements)
5. [Non-Functional Requirements](#5-non-functional-requirements)
6. [Technical Architecture](#6-technical-architecture)
7. [Infrastructure Setup](#7-infrastructure-setup)
8. [Test Definitions](#8-test-definitions)
9. [Open Questions](#9-open-questions)
10. [Build Milestones](#10-build-milestones)

---

## 1. Overview

PiCam Bot is a lightweight Go service running on a Raspberry Pi 4 that provides on-demand, Telegram-controlled camera monitoring. The system is off by default — the camera and motion daemon are dormant until explicitly activated by an authorised Telegram user. Once active, the operator can request snapshots, timed video recordings, or live motion detection, all delivered directly to Telegram.

The service is designed for a single-operator home or small-office context, exposed only over Tailscale with no public internet surface area.

---

## 2. Goals & Non-Goals

### 2.1 Goals

- Zero-idle resource usage: camera and motion daemon fully off in sleep mode.
- Single operator control via Telegram bot with `chat_id` whitelist.
- All traffic over Tailscale; no public port exposure.
- Minimal process footprint: one Go binary + motion daemon, no middleware.
- Deterministic state machine: no ambiguous intermediate states.
- Graceful restart: state persisted to disk so reboots are transparent.

### 2.2 Non-Goals

- Multi-user or role-based access.
- Web UI or dashboard.
- Cloud storage or long-term video archival.
- Multiple simultaneous camera streams.
- Public internet exposure.

---

## 3. User Stories

| ID | As a user I want to… | So that… |
|----|----------------------|----------|
| US-01 | Send `/start` to wake the camera system | I can begin monitoring without SSH-ing into the Pi. |
| US-02 | Send `/snapshot` to receive an immediate still image | I can visually check the scene on demand. |
| US-03 | Send `/record 30` to receive a 30-second video clip | I can capture a short event without continuous recording. |
| US-04 | Send `/detect` to enable motion detection | I am notified automatically when movement occurs. |
| US-05 | Receive a Telegram alert with a thumbnail when motion is detected | I am immediately aware of events without polling. |
| US-06 | Send `/stop` to return the system to sleep | The camera and daemon are fully powered down when not needed. |
| US-07 | Send `/status` to query the current system state | I know whether the system is sleeping, monitoring, or detecting. |
| US-08 | Be the only person able to issue commands | No unauthorised user can operate the camera. |

---

## 4. Functional Requirements

### 4.1 State Machine

Four states:

| State | Description |
|-------|-------------|
| `sleep` | motion daemon stopped; `/dev/video0` released; camera LED off. |
| `monitoring` | motion daemon running, detection paused; awaiting commands. |
| `recording` | Transient during `/record N`; returns to `monitoring` on completion. |
| `detecting` | Motion detection active; events forwarded to Telegram. |

Valid transitions:

| From | Trigger | To |
|------|---------|----|
| `sleep` | `/start` | `monitoring` |
| `monitoring` | `/snapshot` | `monitoring` (unchanged) |
| `monitoring` | `/record N` | `recording` → `monitoring` |
| `monitoring` | `/detect` | `detecting` |
| `detecting` | `/stopdetect` | `monitoring` |
| `monitoring` | `/stop` | `sleep` |
| `detecting` | `/stop` | `sleep` |
| `recording` | `/stop` (during clip) | `sleep` (clip discarded) |

### 4.2 Commands

| Command | Behaviour | Response |
|---------|-----------|----------|
| `/start` | Starts motion via systemctl; transitions to `monitoring`. | ✅ Camera online. Ready for commands. |
| `/stop` | Pauses detection if active; stops motion via systemctl; transitions to `sleep`. | ✅ Camera offline. System sleeping. |
| `/snapshot` | Calls motion webcontrol snapshot API; sends JPEG to Telegram. | Photo with timestamp caption. |
| `/record N` | Records N seconds (1–300); sends MP4 to Telegram. | Video with duration caption. Error if N out of range. |
| `/detect` | Resumes detection via webcontrol; starts fsnotify watcher on `target_dir`. | ✅ Motion detection enabled. |
| `/stopdetect` | Pauses detection; stops fsnotify watcher. | ✅ Motion detection disabled. |
| `/status` | Returns current state, uptime, last event timestamps. | Status message (see §4.3). |
| `/help` | Lists all commands with brief descriptions. | Formatted command list. |

### 4.3 /status Response Format

```
🟢 State: monitoring
⏱ Uptime: 2h 14m
📷 Last snapshot: 14:32:01
🎥 Last recording: 14:28:44 (30s)
👁 Last motion event: —
```

### 4.4 Motion Detection Alerts

New `*.jpg` in `target_dir`:
```
🚨 Motion detected — 14:47:03
```

New `*.mp4` in `target_dir`:
```
🎥 Motion clip saved — 14:47:03
```

### 4.5 Authorisation

- Only messages where `message.From.ID` matches an entry in `ALLOWED_USER_IDS` are processed.
- Checking `From.ID` (sender) rather than `Chat.ID` (conversation) means authorisation works correctly in both DMs and group chats — only whitelisted users can issue commands regardless of context.
- All other senders receive no response (silent drop, logged at `WARN`).
- `ALLOWED_USER_IDS` is a comma-separated list of Telegram user IDs set via environment variable at startup.

---

## 5. Non-Functional Requirements

| Category | Requirement |
|----------|-------------|
| Startup latency | Bot ready within 3s of systemd start. |
| Command latency | `/snapshot` response delivered within 5s. |
| Recording limit | `/record N` capped at 300s max; 1s min. |
| Resource usage | < 20 MB RSS in `sleep` state. |
| Crash recovery | systemd `Restart=always`; state file re-read on startup. |
| Logging | Structured JSON via `log/slog`; level configurable via `LOG_LEVEL`. |
| Security | No public ports; Tailscale-only; single-chat auth. |
| Platform | Debian Bookworm arm64, Raspberry Pi 4, Go 1.24. |

---

## 6. Technical Architecture

### 6.1 Component Overview

| Component | Responsibility |
|-----------|---------------|
| Bot (Go binary) | Telegram polling, command dispatch, state machine, motion lifecycle via systemctl. |
| `MotionClient` | HTTP client wrapping motion webcontrol API (snapshot, record, detection control). |
| `EventWatcher` | fsnotify watcher on `target_dir`; emits events to Notifier. |
| `Notifier` | Sends photos, videos, and text to Telegram. |
| `StateStore` | Reads/writes current State to JSON file for crash recovery. |
| motion daemon | External process managed via systemctl; owns `/dev/video0`; provides webcontrol HTTP API. |

### 6.2 Package Structure

```
picambot/
├── main.go
├── config.go
├── internal/
│   ├── bot/         # Telegram handler, command dispatch
│   ├── state/       # FSM + StateStore
│   ├── motion/      # MotionClient (HTTP) + systemctl wrapper
│   ├── watcher/     # EventWatcher (fsnotify)
│   └── notify/      # Telegram send helpers
├── scripts/        # Deployable audio hook scripts
└── AGENTS.md
```

### 6.3 Key Interfaces

```go
type StateStore interface {
    Load() (State, error)
    Save(State) error
}

type MotionClient interface {
    Start() error
    Stop() error
    Snapshot() (string, error)          // returns path to JPEG
    Record(seconds int) (string, error) // returns path to MP4
    DetectionStart() error
    DetectionPause() error
}

type EventWatcher interface {
    Watch(dir string, out chan<- WatchEvent) error
    Stop()
}
```

### 6.4 Configuration (Environment Variables)

| Variable | Required | Description |
|----------|----------|-------------|
| `TELEGRAM_TOKEN` | Yes | Bot token from @BotFather. |
| `ALLOWED_USER_IDS` | Yes | Comma-separated list of authorised Telegram user IDs (checked against `From.ID`). |
| `MOTION_HOST` | No | motion webcontrol host (default: Tailscale IP). |
| `MOTION_PORT` | No | motion webcontrol port (default: `7999`). |
| `MOTION_TARGET_DIR` | No | Path motion writes output to (default: `/var/lib/motion`). |
| `STATE_FILE` | No | Persisted state path (default: `/var/lib/picambot/state.json`). |
| `LOG_LEVEL` | No | `debug\|info\|warn\|error` (default: `info`). |

### 6.5 motion Configuration

Relevant `/etc/motion/motion.conf` settings:

```ini
videodevice /dev/video0
width 1280
height 720
framerate 30

# Webcontrol — bind to Tailscale IP
webcontrol_localhost off
webcontrol_ip 100.x.x.x
webcontrol_port 7999
webcontrol_parms 2

# Stream
stream_localhost off
stream_ip 100.x.x.x
stream_port 8081

# Output
output_pictures best
ffmpeg_output_movies on
target_dir /var/lib/motion

# Start paused — bot controls detection
pause on
```

---

## 7. Infrastructure Setup

### 7.1 Dependencies

- `motion` (`apt install motion`)
- Go 1.24 toolchain
- Tailscale (already running)
- sudoers rule for `systemctl start/stop motion`

### 7.2 Systemd Services

`motion.service` — **not** enabled at boot; bot manages lifecycle:

```ini
[Unit]
Description=motion camera daemon
After=network.target

[Service]
ExecStart=/usr/bin/motion -c /etc/motion/motion.conf
Restart=on-failure

[Install]
# Do NOT enable — bot starts/stops this unit
```

`picambot.service` — enabled at boot:

```ini
[Unit]
Description=PiCam Telegram Bot
After=network.target tailscaled.service

[Service]
EnvironmentFile=/etc/picambot/env
ExecStart=/usr/local/bin/picambot
Restart=always
User=youruser

[Install]
WantedBy=multi-user.target
```

### 7.3 Sudoers Rule

```
# /etc/sudoers.d/picambot
youruser ALL=(ALL) NOPASSWD: /bin/systemctl start motion, /bin/systemctl stop motion
```

### 7.4 Directory Layout

```
/etc/picambot/env                 # env vars (chmod 600)
/var/lib/picambot/state.json      # persisted FSM state
/var/lib/motion/                  # motion output (clips + frames)
/usr/local/bin/picambot           # compiled binary
```

---

## 8. Test Definitions

### 8.1 Unit Tests

| ID | Target | Assertion |
|----|--------|-----------|
| UT-01 | FSM transitions | All valid transitions succeed; invalid ones return `ErrInvalidTransition`. |
| UT-02 | FSM transitions | `/stop` from `sleep` returns `ErrInvalidTransition`. |
| UT-03 | `StateStore` | Save → Load round-trips all State values. |
| UT-04 | `MotionClient` | `Snapshot()` calls correct webcontrol URL and returns file path. |
| UT-05 | `MotionClient` | `Record(N)` with N > 300 returns `ErrOutOfRange`. |
| UT-06 | Auth | Message from non-whitelisted `From.ID` is silently dropped; no send occurs. |
| UT-07 | Auth | Message from whitelisted `From.ID` is dispatched to handler, whether sent in DM or group. |
| UT-08 | `EventWatcher` | New `*.jpg` in watched dir emits `PhotoEvent`. |
| UT-09 | `EventWatcher` | New `*.mp4` in watched dir emits `VideoEvent`. |
| UT-10 | Config | Missing `TELEGRAM_TOKEN` or `ALLOWED_USER_IDS` causes startup error with clear message. |

### 8.2 Integration Tests

| ID | Scope | Assertion |
|----|-------|-----------|
| IT-01 | `/start` | motion daemon starts; state → `monitoring`; Telegram receives success message. |
| IT-02 | `/snapshot` | motion snapshot API called; JPEG sent to Telegram within 5s. |
| IT-03 | `/record 10` | motion record API called; MP4 sent to Telegram after clip completes. |
| IT-04 | `/detect` | Detection enabled via webcontrol; state → `detecting`. |
| IT-05 | Motion event | File dropped into `target_dir` triggers Telegram notification. |
| IT-06 | `/stop` from `detecting` | Detection paused; motion stopped; state → `sleep`. |
| IT-07 | Restart recovery | Bot restarts with `detecting` in `state.json`; resumes correctly. |

### 8.3 Manual Acceptance Tests

| Scenario | Pass Criteria |
|----------|---------------|
| Cold start on Pi reboot | `picambot` starts; `/status` returns `sleep`; camera LED off. |
| Full wake → detect → alert → sleep cycle | All transitions complete; motion alert photo received; `/stop` returns to sleep with LED off. |
| Unauthorised command | Non-whitelisted user in DM or group gets no response; `WARN` entry visible in `journalctl`. |
| `/record 301` boundary | Bot replies with error; no recording starts. |
| Pi power loss during `detecting` | On restart, bot reads `detecting` from state file; sends "Resumed monitoring"; detection re-armed. |

---

## 9. Open Questions

- **Clip retention**: how long before files in `target_dir` are pruned? Manual or cron?
- **Telegram file size limit**: bot API caps uploads at 50 MB. At 720p H.264 (~1 MB/s), safe ceiling is ~50s. Should `/record N` clamp silently or return an error?
- **Crash recovery messaging**: should the bot proactively message the operator on restart, or resume silently?
- **`/stream` command**: send the motion MJPEG stream URL (`stream_port 8081`) as a clickable Tailscale link?

---

## 10. Build Milestones

| Milestone | Scope |
|-----------|-------|
| M1 — Skeleton | Go module, config, `log/slog` setup, Telegram polling, auth filter, `/status` returning hardcoded `sleep`. |
| M2 — State machine | FSM + `StateStore`; `/start` and `/stop` wired to systemctl; state persisted. |
| M3 — Snapshot | `/snapshot` wired to motion webcontrol; JPEG sent to Telegram. |
| M4 — Recording | `/record N` wired; MP4 sent; range validation. |
| M5 — Detection | `/detect` and `/stopdetect`; `EventWatcher` sending motion alerts to Telegram. |
| M6 — Hardening | Unit + integration tests; crash recovery test; README + `AGENTS.md`. |

---

*— end of specification —*

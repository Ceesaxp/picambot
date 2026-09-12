# TODO

## Completed

- [x] M1 — Skeleton: go.mod, config.go, main.go, Telegram polling, auth filter, /status
- [x] M2 — State machine: FSM + StateStore, /start /stop wired to systemctl
- [x] M3 — Snapshot: /snapshot wired to motion webcontrol; JPEG sent to Telegram
- [x] M4 — Recording: /record N wired; MP4 sent; range validation
- [x] M5 — Detection: /detect /stopdetect; EventWatcher; crash recovery
- [x] M6 — Hardening: unit + integration tests; AGENTS.md
- [x] Deploy to Raspberry Pi 4 and run manual acceptance tests (spec §8.3)
- [x] Configure systemd units and sudoers rule (spec §7.2–7.3)
- [x] Configure motion.conf (spec §6.5)
- [x] Address open questions from spec §9 (clip retention, Telegram file size, crash recovery messaging, /stream command)
- [x] Add HomeKit integration

## Remaining

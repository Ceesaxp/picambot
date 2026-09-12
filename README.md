# PiCamBot

A lightweight Go service for Raspberry Pi 4 that provides on-demand, Telegram-controlled camera monitoring. The camera and motion daemon are off by default — everything is activated explicitly through Telegram commands or an optional Apple Home switch.

Motion access uses Tailscale. The optional Apple Home switch uses the local network; no public ports are needed.

---

## Commands

| Command | Description |
|---------|-------------|
| `/run` | Wake the camera system |
| `/stop` | Return to sleep |
| `/snapshot` | Capture and send a still image |
| `/record <N>` | Record N seconds of video (1–300) |
| `/detect` | Enable motion detection alerts |
| `/stopdetect` | Disable motion detection |
| `/status` | Show current state and uptime |
| `/help` | List all commands |

---

## Configuration

The bot is configured entirely through environment variables. On the Pi these live in `/etc/picambot/env` (loaded by the systemd unit). For local development, source the example file.

Copy `env.example` to get started:

```sh
cp env.example /etc/picambot/env
chmod 600 /etc/picambot/env
```

### Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TELEGRAM_TOKEN` | **Yes** | — | Bot token from [@BotFather](https://t.me/BotFather). |
| `ALLOWED_USER_IDS` | **Yes** | — | Comma-separated Telegram user IDs. Checked against the *sender's* ID (`From.ID`), so it works in both DMs and groups. Find yours via [@userinfobot](https://t.me/userinfobot). |
| `MOTION_HOST` | No | `127.0.0.1` | IP address of the motion webcontrol API. Use your Tailscale IP (`100.x.x.x`) when motion is bound to the Tailscale interface. |
| `MOTION_PORT` | No | `7999` | Port for the motion webcontrol API (`webcontrol_port` in motion.conf). |
| `MOTION_TARGET_DIR` | No | `/var/lib/motion` | Directory where motion writes output files. The bot watches this for new `*.jpg` and `*.mp4` files during detection mode. |
| `STATE_FILE` | No | `/var/lib/picambot/state.json` | Path to the JSON file used to persist FSM state across restarts. Parent directory is created automatically. |
| `LOG_LEVEL` | No | `info` | Log verbosity: `debug`, `info`, `warn`, or `error`. Logs are JSON, written to stdout, captured by journald. |

---

## Apple Home switch (optional)

PiCam Bot can appear directly in Apple Home as **Camera monitoring**, using native HomeKit (HAP). Homebridge is not required.

- **On:** wake the camera and enable motion detection (`/run` + `/detect`).
- **Off:** stop the camera (`/stop`), including cancelling a timed recording.
- The switch shows **On only while detection is active**. Telegram commands update its status too. After `/run` alone or `/stopdetect`, it shows Off even though the camera is awake; sending Off still stops the camera.
- Turning On during a timed recording returns an error; stop the camera first.

To enable it, add these settings to `/etc/picambot/env`:

```ini
HOMEKIT_ENABLED=true
HOMEKIT_PIN=<your eight-digit pairing code>
HOMEKIT_NAME="Camera monitoring"
```

Choose eight digits without separators; repeated digits and `12345678` / `87654321` are rejected. Restart `picambot`, then use **Home → Add Accessory → More Options**, select **Camera monitoring**, and enter your pairing code. Accept the uncertified-accessory prompt if shown.

The iPhone and Pi must have local-network connectivity with mDNS discovery (UDP 5353) and TCP 51826 allowed. Ordinary Tailscale connectivity alone does not provide local mDNS discovery. HomeKit controls are authorized by Apple Home pairing and home membership, independently of Telegram's `ALLOWED_USER_IDS`.

Pairing data is stored in `homekit/` beside `STATE_FILE`, normally `/var/lib/picambot/homekit`. The service user must be able to write there. Preserve it across deployments so the accessory keeps its identity. Override with `HOMEKIT_STORAGE_DIR` if needed. Disabling `HOMEKIT_ENABLED` stops the HomeKit listener without deleting pairings.

This is a control switch: photos and clips continue to go to Telegram. Use an authorized Telegram command at least once to establish the destination chat; HomeKit commands preserve that destination and can control the camera before one is set. If enabling detection fails after the camera has started, the camera remains awake with the switch Off; retry On or send Off to stop it. Failed hardware operations are reported to Home, and failed stops remain retryable.

---

## Building

```sh
# Build for the current platform (development)
make build

# Cross-compile for Raspberry Pi 4 (Linux arm64) — the primary target
make build-pi

# Other platforms
make build-linux-amd64
make build-darwin-arm64
make build-darwin-amd64

# All platforms at once
make build-all
```

### Deploy to Pi over SSH

```sh
make deploy PI_HOST=pi@raspberrypi
```

This cross-compiles, installs the binary, systemd units from `docs/`, and audio hook scripts from `scripts/`, then restarts the systemd unit.

To update just the audio hooks, run `make deploy-scripts PI_HOST=pi@raspberrypi`. Use `make check-deploy PI_HOST=pi@raspberrypi` to inspect the deployed hooks and compare script checksums.

---

## Setup

### 1. Install dependencies on the Pi

```sh
sudo apt install motion
```

Tailscale must already be running.

### 2. Configure motion

Copy the example config and edit the Tailscale IP:

```sh
sudo cp docs/motion.conf.example /etc/motion/motion.conf
sudo nano /etc/motion/motion.conf   # replace 100.x.x.x with your Tailscale IP
```

The audio hooks live in `scripts/`. For manual setup, install them at the paths used by the motion configuration:

```sh
sudo install -m 0755 scripts/picambot-audio-start scripts/picambot-audio-stop /usr/local/bin/
```

### 3. Configure the bot

```sh
sudo mkdir -p /etc/picambot
sudo cp env.example /etc/picambot/env
sudo chmod 600 /etc/picambot/env
sudo nano /etc/picambot/env         # set TELEGRAM_TOKEN and ALLOWED_USER_IDS
```

### 4. Sudoers rule

The bot controls the motion daemon via `systemctl`. Grant it passwordless access:

```sh
sudo tee /etc/sudoers.d/picambot <<'EOF'
picambot ALL=(root) NOPASSWD: /usr/bin/systemctl start motion, /usr/bin/systemctl stop motion
EOF
sudo chmod 440 /etc/sudoers.d/picambot
```

> **Note:** verify the path with `which systemctl` on the Pi — it should be `/usr/bin/systemctl` on Raspberry Pi OS (Debian bookworm). Sudoers matches on the real path, not symlinks.

### 5. Create service user

The bot runs as a dedicated low-privilege user with access to `/dev/video0`:

```sh
sudo useradd --system --no-create-home picambot
sudo usermod -aG video picambot
```

### 6. Systemd units

Ready-to-use unit files are in `docs/`. Copy them to the systemd directory:

```sh
sudo cp docs/picambot.service /etc/systemd/system/picambot.service
sudo cp docs/motion.service   /etc/systemd/system/motion.service
sudo systemctl daemon-reload
sudo systemctl enable --now picambot
```

`motion.service` has no `[Install]` section — it is **not** enabled at boot. The bot starts and stops it on demand via `systemctl`.

---

## Directory layout on the Pi

```
/etc/picambot/env                 # env vars (chmod 600)
/etc/motion/motion.conf           # motion daemon config
/var/lib/picambot/state.json      # persisted FSM state (auto-created)
/var/lib/motion/                  # motion output: clips + frames
/usr/local/bin/picambot           # compiled binary
```

---

## Development

```sh
# Run unit tests
make test

# Run integration tests (no external services required)
make test-integration

# Run everything
make test-all

# Local run (source env vars first)
set -a && source env.example && set +a
go run .
```

---

## State machine

```
sleep ──/run──► monitoring ──/detect──► detecting
                      │                      │
                   /record               /stopdetect
                      │                      │
                  recording ─────────────► monitoring
                      │
              /stop (from any state) ──► sleep
```

`/stop` from `sleep` is rejected. All other invalid transitions are silently blocked with an error reply.

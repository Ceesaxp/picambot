# PiCamBot

PiCam Bot is a lightweight Go service running on a Raspberry Pi 4 that provides on-demand, Telegram-controlled camera monitoring. The system is off by default — the camera and motion daemon are dormant until explicitly activated by an authorised Telegram user. Once active, the operator can request snapshots, timed video recordings, or live motion detection, all delivered directly to Telegram.

The service is designed for a single-operator home or small-office context, exposed only over Tailscale with no public internet surface area.


BINARY   := picambot
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION) -s -w"

# Default target: build for the current host platform.
.PHONY: build
build:
	go build $(LDFLAGS) -o $(BINARY) .

# Cross-compile for Raspberry Pi 4 (Linux arm64).
.PHONY: build-pi
build-pi:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-linux-arm64 .

# Cross-compile for Linux amd64 (CI, VMs).
.PHONY: build-linux-amd64
build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-linux-amd64 .

# Cross-compile for macOS arm64 (Apple Silicon).
.PHONY: build-darwin-arm64
build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-darwin-arm64 .

# Cross-compile for macOS amd64 (Intel Mac).
.PHONY: build-darwin-amd64
build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-darwin-amd64 .

# Build all supported platforms.
.PHONY: build-all
build-all: build-pi build-linux-amd64 build-darwin-arm64 build-darwin-amd64

.PHONY: test
test:
	go test -race ./...

.PHONY: test-integration
test-integration:
	go test -race -tags integration ./...

.PHONY: test-all
test-all: test test-integration

.PHONY: vet
vet:
	go vet ./...

# Deploy the Pi binary, unit files, and audio hook scripts to the Pi over SSH.
# Usage: make deploy PI_HOST=pi@raspberrypi
# SSH_OPTS can override ssh/scp options (e.g. to disable RemoteCommand).
SSH_OPTS ?= -o RemoteCommand=none
.PHONY: deploy
deploy: build-pi
	@test -n "$(PI_HOST)" || (echo "Usage: make deploy PI_HOST=pi@raspberrypi" && exit 1)
	scp $(SSH_OPTS) $(BINARY)-linux-arm64 docs/picambot.service docs/motion.service docs/picambot-audio-start docs/picambot-audio-stop $(PI_HOST):/tmp/
	ssh $(SSH_OPTS) $(PI_HOST) "\
		sudo install -m 0755 /tmp/$(BINARY)-linux-arm64 /usr/local/bin/$(BINARY) && \
		sudo install -m 0755 /tmp/picambot-audio-start /usr/local/bin/picambot-audio-start && \
		sudo install -m 0755 /tmp/picambot-audio-stop  /usr/local/bin/picambot-audio-stop && \
		sudo install -m 0644 /tmp/picambot.service /etc/systemd/system/picambot.service && \
		sudo install -m 0644 /tmp/motion.service   /etc/systemd/system/motion.service && \
		rm -f /tmp/$(BINARY)-linux-arm64 /tmp/picambot.service /tmp/motion.service /tmp/picambot-audio-start /tmp/picambot-audio-stop && \
		sudo systemctl daemon-reload && \
		sudo systemctl restart picambot"

# Deploy only the binary (no service files or scripts) and restart.
# Usage: make deploy-bin PI_HOST=pi@raspberrypi
.PHONY: deploy-bin
deploy-bin: build-pi
	@test -n "$(PI_HOST)" || (echo "Usage: make deploy-bin PI_HOST=pi@raspberrypi" && exit 1)
	scp $(SSH_OPTS) $(BINARY)-linux-arm64 $(PI_HOST):/tmp/
	ssh $(SSH_OPTS) $(PI_HOST) "sudo install -m 0755 /tmp/$(BINARY)-linux-arm64 /usr/local/bin/$(BINARY) && rm -f /tmp/$(BINARY)-linux-arm64 && sudo systemctl restart picambot"

# Deploy only the audio hook scripts (no binary, no service files).
# Usage: make deploy-scripts PI_HOST=pi@raspberrypi
.PHONY: deploy-scripts
deploy-scripts:
	@test -n "$(PI_HOST)" || (echo "Usage: make deploy-scripts PI_HOST=pi@raspberrypi" && exit 1)
	scp $(SSH_OPTS) docs/picambot-audio-start docs/picambot-audio-stop $(PI_HOST):/tmp/
	ssh $(SSH_OPTS) $(PI_HOST) "\
		sudo install -m 0755 /tmp/picambot-audio-start /usr/local/bin/picambot-audio-start && \
		sudo install -m 0755 /tmp/picambot-audio-stop  /usr/local/bin/picambot-audio-stop && \
		rm -f /tmp/picambot-audio-start /tmp/picambot-audio-stop"

# Check the deployed motion.conf hook lines and script checksums against the repo.
# Usage: make check-deploy PI_HOST=pi@raspberrypi
.PHONY: check-deploy
check-deploy:
	@test -n "$(PI_HOST)" || (echo "Usage: make check-deploy PI_HOST=pi@raspberrypi" && exit 1)
	@echo "── motion.conf hooks (deployed) ────────────────────────────────────────"
	@ssh $(SSH_OPTS) $(PI_HOST) "grep -nE '^(on_event_start|on_event_end|on_movie_end|on_picture_save|movie_filename)' /etc/motion/motion.conf"
	@echo "── script checksums (deployed vs repo) ─────────────────────────────────"
	@ssh $(SSH_OPTS) $(PI_HOST) "sha256sum /usr/local/bin/picambot-audio-start /usr/local/bin/picambot-audio-stop" | sed 's|/usr/local/bin/|deployed:  |'
	@shasum -a 256 docs/picambot-audio-start docs/picambot-audio-stop | sed 's|docs/|repo:      |'

.PHONY: clean
clean:
	rm -f $(BINARY) $(BINARY)-linux-arm64 $(BINARY)-linux-amd64 $(BINARY)-darwin-arm64 $(BINARY)-darwin-amd64

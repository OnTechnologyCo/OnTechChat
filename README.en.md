# OnTechChat — single working directory

[한국어](README.md) | **English**

Project root: `D:\OnMakeSrc\OnTechChat`.
Other agents must read **AGENTS.md → docs/WORK_STATUS.md**, in that order, before working.

| Purpose | Official path |
|---|---|
| Android 1.8.11 source | android/OnTechChat/ |
| Windows server 1.15.35 source | server/ |
| Integrated installer / tray | installer/ / tray/ |
| Only build entry point | build.ps1 |
| Distribution APK | output/OnTechChat-v1.8.11-arm64.apk |
| Server installer | output/OnTechChat-Server-Setup-v1.15.35-x64.exe |
| Distribution file hashes | output/manifest.json / SHA256SUMS.txt |
| Build intermediates and logs | .build/ |
| Previous versions | archive/ (do not modify, build, or run) |

Naver mail forwarding accepts multiple subject phrases, one per line. Any matching phrase forwards the message once. Server and Android updates use files from official GitHub releases. See the [release notes](docs/server/RELEASE-1.15.35.md) and docs/WORK_STATUS.md.

Server Setup 1.15.35 includes the official Android 1.8.11 APK in its internal payload. On a new installation or update, it automatically publishes the APK to the server's `DataDir\apk-releases`. The Android app downloads the APK through the existing update API, verifies it, and then opens the operating system's installation screen.

Run these commands in PowerShell:

- Full validation and build: `.\build.ps1 -Target All`
- Android validation and build: `.\build.ps1 -Target Android`
- Server validation and build: `.\build.ps1 -Target Server`
- Tests: `.\build.ps1 -Target Tests`
- Emulator: `.\build.ps1 -Target Emulator` (does not write to the distribution directory)

The solution is for opening the official app source. Use the script above for builds.
Output filenames include the version, and `output` contains only the latest file of each type. Do not create source or distribution folders for individual versions.
The default `-Validation Core` runs only the core checks. Use `-Validation Full` only when a complete regression run is needed.
Use only `scripts/share.py` for downloads; check `.build/download.json` for the current address.
The temporary download address stops working when the process exits. Automatic startup after a reboot has not been configured.
The installed production server, Caddy, data, and firewall were not changed by the directory cleanup described here.

**1.15.12:** Removed the model/reasoning selector at the top of AI conversations; added clearing one's own direct-chat history, general-purpose Codex/Claude web search, and administrator RemoteControl MCP integration.
See [docs/server/AI-CLI-AND-CHAT.md](docs/server/AI-CLI-AND-CHAT.md) for CLI installation on another PC, service-account authentication, and deletion scope.

**1.15.11:** Prepares the Codex runtime in a shared location on the installation PC. After installation, authenticate AI once using the server's Codex login. Changing the Windows service account or entering a Windows password is unnecessary. Personal Codex credentials are not copied, and server authentication is retained after updates. See [docs/server/AI-CLI-AND-CHAT.md](docs/server/AI-CLI-AND-CHAT.md).

**1.15.13:** Added selecting friends/joined conversations → previewing the analysis scope → AI summaries, to-dos, reply drafts, and questions → navigating to evidence and using drafts. Reduced the bottom spacing of the mobile input box. See [docs/server/CONVERSATION-HELP.md](docs/server/CONVERSATION-HELP.md) for usage, retention, and limits.

**1.15.14 / Android 1.8.0 (180):** Added GPS recording at one-minute intervals, 48-hour retention, an encrypted local queue and server retries, date/time route maps, and separate controls for friend sharing, stopping, and deletion. See [docs/server/LOCATION-HISTORY.md](docs/server/LOCATION-HISTORY.md). Update both the server and the app.

**1.15.15 / Android 1.8.1 (181):** Added AI file/image attachments, Codex image editing, a per-room automatic AI-help checkbox, mapped location routes (the most recent hour by default), automatic recording based on the existing location permission, and automatic app-update downloads. See [docs/server/RELEASE-1.15.15.md](docs/server/RELEASE-1.15.15.md).


Configure automatic domain HTTPS in Server settings → External access. The server obtains and renews a public certificate on TCP 443 while preserving existing IP access. DNS-only mode and inbound public TCP 443 are required. [Setup guide](docs/server/RELEASE-1.15.34.md).

Windows releases use Inno Setup exclusively (installer/OnTechChat.iss, build.config.json innoSetup). The previous standalone setup UI and publishing path were removed. The embedded runtime retains service/data migration, rollback and the single existing uninstall registration. Automatic domain HTTPS is configured in Server Settings after installation.

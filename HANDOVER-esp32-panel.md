# Session-Handover — ESP32 Scan Control Panel

> **Lokale Übergabenotiz** (untracked, bewusst nicht committet). Source of Truth sind die
> GitHub-Issues **#7** und **#9**. Stand: 2026-06-28.

## Worum es geht
Wiederverwendung eines **ESP32-2432S028 (CYD, „Cheap Yellow Display")** als **hardware-unabhängiger
Scan-Trigger + Profil-Auswahl** für paperless-scan-bridge — Alternative/Fallback zum SANE-opaken
Scanner-Start-Button.

## Wo wir stehen (nur geplant, **noch kein Code** für dieses Feature)
- **Issue #7** (Hardware-Event-Detection beyond SANE): Kommentar ergänzt — ESP32-Panel als
  hardware-unabhängiger Trigger/Fallback.
- **Issue #9** (NEU, vollständige Spec „ESP32 (CYD) scan control panel"): enthält
  - Architektur + Layering (ESP32 ↔ `scan-bridge` HTTP; Scanner ↔ `sane-runtime` USB),
  - **API-Planung** (Endpoints/Auth/Shapes),
  - **Profil-Modell-Erweiterung**: `display_order`, `display_enabled`, `color`, `label`,
  - **Container-UI** mit **Drag-and-Drop-Sortierung**,
  - **Display-Mockups** (Hoch- **und** Querformat) + Zustände,
  - **On-Device-Webservice** am ESP32 (wie BambuHelper) für Geräteeinstellungen,
  - **First-Boot-Provisioning**: Improv Wi-Fi (BLE/Serial) + SoftAP-Captive-Portal-Fallback,
  - **Build + Web-Installer** (ESP Web Tools; BambuHelper als Referenz),
  - Auth, offene Entscheidungen (→ ADRs), **Phasen A–E**.

## Verifizierte Repo-Fakten (Basis der Planung)
- **Container-first/host-thin** (ARCHITECTURE.md, CONTAINER_SUITE.md):
  - `sane-runtime` „owns the scanner" via `--device=/dev/bus/usb` (+ udev, kein `--privileged`).
  - `scan-bridge` (Go) = HTTP-API + Dispatch (Unix-Socket zu `sane-runtime`).
  - `scan-processor` → Paperless-ngx.
- **Auth**: Token-Mode (Bearer, SHA-256 `TokenHash`) bzw. IP-Allowlist (`internal/config/config.go`).
- **Profile**: `components/scan-bridge/internal/profiles/` (`defaults.yaml`): `private-duplex`,
  `private-simplex`, `receipts`, `archive`.
- **API heute**: `GET /profiles`, `GET /profiles/{name}` live; `POST /scan`, `GET /jobs/{id}` noch
  `notImplemented` (brauchen Dispatch/Job-Store).
- **ESP32 kann den Host nicht ersetzen** (kein USB-Host für Vendor-Scanner, kein SANE/`avision` auf
  MCU, RAM/Bildpipeline) — Host muss aber **kein Pi** sein (jeder Linux-Host mit Scanner am USB).

## Nächste Schritte (Vorschlag aus #9)
- **Phase A**: Profil-Modell um `display_order`/`display_enabled`/`color`/`label` erweitern;
  `GET /profiles` liefert **vorsortiert + gefiltert**; Tests.
- **Phase B**: Container-UI (Profil-CRUD + Drag-and-Drop-Reihenfolge).
- **Phase C**: `POST /scan` + `GET /jobs/{id}` (hängt an Dispatch/Job-Store).
- **Phase D**: CYD/LVGL-Firmware (lesen→rendern→auslösen→Status; hoch/quer; On-Device-Web-UI; Improv).
- **Phase E**: CI-Firmware-Image + ESP-Web-Tools-Installer + Docs.

## Querbezug
Gleiches **CYD/LVGL→REST-Firmware-Muster** wie der ESP32-Slot-Selektor in **BambuBridge**
(`~/Repos/BambuBridge`) — Projekte bleiben eigenständig, nur das *Muster/Skelett* wird geteilt.

## Umgebung
- `strausmann` hat `gh`/git mit GH-Token konfiguriert (PRs/Issues möglich).
- Dieses Repo folgt eigener Governance (`CLAUDE.md`, `AGENTS.md`, `CONTRIBUTING.md`, ADRs in `docs/`).

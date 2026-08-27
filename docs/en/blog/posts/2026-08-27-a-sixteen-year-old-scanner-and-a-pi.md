---
title: "A sixteen-year-old scanner, a Pi, and the button that does not exist"
date: 2026-08-27
authors:
  - strausmann
categories:
  - project
tags:
  - phase-1
  - hardware
  - sane
description: "The Phase 1 pipeline scans, assembles and uploads a real document. Getting there meant discarding the feature the whole design was named after."
---

# A sixteen-year-old scanner, a Pi, and the button that does not exist

The plan was one sentence long: put a document in the scanner, press the
button on the scanner, find it in Paperless-ngx thirty seconds later.
The button turned out not to exist — not in any sense software can
reach — and finding that out changed the shape of everything else.

<!-- more -->

## What works today

On 26 August 2026 the whole path ran against real hardware for the first
time. A `POST /scan` pulled a two-sided sheet through the feeder of a
**Kodak ScanMate i1120**, `scan-processor` assembled a two-page PDF, and
Paperless-ngx accepted it with the right tags and the right date. Three
containers on a Raspberry Pi 5, nothing installed on the host but
Docker, an NFS mount and a udev rule.

The scanner is from 2009. Kodak never shipped a Linux driver for it and
never will. It works through SANE's `avision` backend, which upstream
has marked unmaintained since 2020 and which nonetheless drives this
device flawlessly on a 6.8 kernel.

## The button that does not exist

The i1120 has a Start button on its front panel. It is a real, physical,
satisfying button. Pressing it produces **nothing** a program can see.

This is not a guess. Over a sixty-second capture with `scanbd` polling
at 250 ms, repeated presses of Start produced **zero** events, while
turning the function wheel next to it produced twenty-one. The wheel
reports as a string on SANE's read-only `--message` option. The button
reports as nothing at all — not through the backend's options, not
through `scanimage -A`, not through direct enumeration.

The ADF paper sensor is equally opaque. Putting paper in changes no
option, no message, no state a caller can read. The only paper-related
signal that exists anywhere is `SANE_STATUS_NO_DOCS`, and you only get
it by *attempting a scan and failing*.

So the original flow — insert paper, scanner starts by itself — is not
achievable on this device. Not "hard": not achievable, through this
interface. `scanbd` came out of the design as a direct result.

## What replaced it

Losing the hardware trigger forced a better question: what should
actually start a scan?

The answer is a single trigger-agnostic endpoint. `POST /scan` takes a
profile name and nothing else, and it does not care who is calling.
A phone shortcut, a Home Assistant automation, an n8n workflow, `curl`,
or — the one that made the difference in daily use — a small touch panel
on the wall.

That panel is an ESP32 with a 2.8" touchscreen that costs less than a
takeaway meal. It lists the profiles the bridge advertises and fires a
scan on a tap. It is not near the scanner. It does not have to be: the
trigger path has nothing to do with physical proximity, which is the
part the original design got wrong by assuming the opposite.

## Three things that cost the most time

**A file mode.** The first authenticated scan against real hardware
worked perfectly and then failed to upload. The Paperless API token was
in a Docker secret with mode `0600`, owned by a host user. The container
runs as UID 65532. It could not read its own credential. The error
surfaced as an upload failure, which sent us looking at Paperless.

**A dropped error.** Enabling the linter for the first time — it had
never run in CI — found `defer f.Close()` unchecked on two `os.Create`
calls. Both wrote files: one a scanned page, one the assembled PDF. On a
writer, `Close` is where the final flush happens. A failure there would
have produced a truncated PDF, uploaded it, and reported `submitted`.
Nobody would have noticed until a document was short a page.

**A touchscreen mapping.** A tap on the panel's top-left corner
registered four pixels from the *bottom*-right. Both axes were inverted
and swapped, so every press on a profile button landed on empty screen.
Diagnosing it needed all four corners: two on the same diagonal cannot
tell you whether the axes are swapped, because both move together.

## What is not done

The pipeline works; the packaging around it is younger. As of this post
the bootstrap script and the published compose stack exist but have not
been run end to end on a machine that was not already set up. Monitoring
and backup are Phase 3. The job store — and with it any history of what
was scanned — is Phase 1.4, which means the panel and the API can both
start a scan but neither can tell you what happened to last Tuesday's.

The scanner still cannot tell us there is paper in it. That one is not
going to be fixed in software.

<!--
  Thanks for the contribution. Please fill in every section. PRs that
  drop the template are likely to bounce. See CONTRIBUTING.md for the
  full workflow.

  Reminder: PR descriptions and review replies are written in German
  for this repository (see CLAUDE.md / .github/copilot-instructions.md).
  Code identifiers, commit-message examples, CLI commands and quoted
  log output stay in their original language.
-->

## Beschreibung

<!--
  Was ändert diese PR und warum? Bei Features: Verweis auf das Issue,
  in dem das Design abgestimmt wurde. Bei Bugfixes: kurz die
  Ursache und der gewählte Fix.
-->

Closes #

## Art der Änderung

<!-- Eine oder mehrere ankreuzen. -->

- [ ] Bugfix (`fix`)
- [ ] Neues Feature (`feat`)
- [ ] Refactor ohne Verhaltensänderung (`refactor`)
- [ ] Dokumentation (`docs`)
- [ ] Tests (`test`)
- [ ] Build / Tooling / CI (`build`, `ci`, `chore`)
- [ ] Performance (`perf`)
- [ ] Security-relevante Änderung — bitte zusätzlich `THREAT_MODEL.md` querprüfen

## Tested how

<!--
  Was wurde tatsächlich ausgeführt? Listenpunkte mit konkreten
  Befehlen und Ergebnissen, nicht "tested locally". Hardware-PRs
  bitte mit Test-Stages aus HARDWARE_COMPATIBILITY.md.
-->

- [ ] `make test-go`
- [ ] `make test-shell`
- [ ] `make test-yaml`
- [ ] `make test-docker`
- [ ] `make test-docs`
- [ ] `make test-ansible` / `make test-molecule` (falls Ansible berührt)
- [ ] Integration-Tests unter `tests/integration/` (falls Compose berührt)

## Checklist

- [ ] Conventional-Commits-Format eingehalten (Scope = Komponente / Verzeichnis).
- [ ] Keine Host-Installationen auf dem Pi eingeführt (Container-first).
- [ ] Keine `latest`-Tags in Compose- oder CI-Dateien.
- [ ] Keine Cloud-Abhängigkeiten auf einem Core-Pfad eingeführt.
- [ ] Errors werden weder geschluckt noch ohne Kontext zurückgegeben.
- [ ] Secrets sind via Env oder SOPS, nicht hartcodiert.
- [ ] `CHANGELOG.md` (`[Unreleased]`) entsprechend ergänzt, falls user-sichtbar.
- [ ] Bei Hardware-Änderungen: `HARDWARE_COMPATIBILITY.md` aktualisiert.

## Screenshots / Logs

<!-- Optional, aber sehr willkommen. -->

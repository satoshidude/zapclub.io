# Zapclub – verbindlicher Projektkontext

## Quellen der Wahrheit

- Dieses Repository enthält ausschließlich Zapclub-bezogene Quellen:
  Anwendungscode, Tests, Dokumentation und projektspezifische
  Produktionsdateien unter `deploy/`.
- `README.md` beschreibt Produkt und Architektur, `deploy/README.md` den
  verbindlichen Betrieb und `release.sh` den ausführbaren Releaseprozess.
- Hostweite Firewall-, SSH-, Kernel-, Caddy-Importer- und Dispatcher-Logik sowie
  der gemeinsame Footer-Vertrag gehören in das Repository `sunnyhill.io`.
  Dieses Repository enthält nur die dazu kompatible Zapclub-Implementierung.

## Aktuelle Produktion

- Zielsystem: `sunnyhill.io`; öffentliche Hosts: `zapclub.io` und
  `relay.zapclub.io`.
- Runtime: `zapclub:zapclub`; persistenter State: `/var/lib/zapclub-relay`;
  Secrets: `/etc/zapclub`.
- Anwendungs-Units: `zapclub-relay`, `zapclub-lnurlp` und
  `zapclub-telegram-bot`.
- Betriebs-Units: `zapclub-backup.service/.timer`,
  `zapclub-monitor.service/.timer` und `zapclub-alert@.service`.
- Releases liegen unter `/srv/zapclub/releases/<commit>`; `current` zeigt auf
  den aktiven Commit. Neben diesem bleibt höchstens ein geprüfter
  Rückfall-Release erhalten.
- Caddy erhält ausschließlich lesenden ACL-Zugriff auf `frontend/dist` und ist
  kein Mitglied von `zapclub`. Nur der Relay-Prozess erhält die Zusatzgruppe
  `caddy`, um das pseudonymisierte Zugriffslog zu lesen.

## Projektlokaler Releasevertrag

- Einstieg ist `./release.sh` aus einem sauberen lokalen `main`.
- Vor dem Push laufen `npm audit`, Frontend-Check/-Tests/-Build, Go Vet/-Tests,
  Relay-E2E und statische Linux-amd64-Builds. Nach der Aktivierung prüft
  `node deploy/smoke.mjs` die öffentlichen Endpunkte und den erwarteten Commit.
- Der Commit wird als exaktes Git-Bundle über `webmaster` übertragen. Der
  hostweite root-eigene Dispatcher validiert, baut und aktiviert ihn atomar;
  seine Implementierung wird nicht in diesem Repository dupliziert.
- Änderungen unter `deploy/` werden nicht automatisch als root installiert.
  Sie sind separat zu validieren und explizit zu installieren; anschließend
  müssen Units, Backup, Monitor, Alarmtransport und öffentliche Endpunkte
  geprüft werden.

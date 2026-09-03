# Zapclub – verbindlicher Projektkontext

Dieses Repository ist die einzige Quelle für Zapclub-Anwendungscode und die
projektspezifischen Produktionsdateien unter `deploy/`. Hostweite Firewall-,
SSH- und Caddy-Importer-Konfiguration gehört in das Repository `sunnyhill.io`.

## Produktion und Deployment

- Ziel: `sunnyhill.io`, öffentliche Hosts `zapclub.io` und `relay.zapclub.io`
- Einstieg: `./release.sh` ausschließlich aus einem sauberen lokalen `main`
- Runtime: `zapclub:zapclub`
- Units: `zapclub-relay`, `zapclub-lnurlp`, `zapclub-telegram-bot`
- Releases: `/srv/zapclub/releases/<commit>`, aktiv über `/srv/zapclub/current`
- State: `/var/lib/zapclub-relay`; Secrets: `/etc/zapclub`
- Prüfungen: Frontend check/test/build, Go vet/test, Relay-E2E und
  `node deploy/smoke.mjs`

Der Release wird vor dem Push vollständig geprüft, als exaktes Git-Bundle über
`webmaster` übertragen und vom root-eigenen Validator gebaut und atomar
aktiviert. Direkte Serveränderungen im aktiven Release, `git pull` in Produktion
oder ein Deployment durch das Runtime-Konto sind verboten. Caddy erhält nur
lesenden ACL-Zugriff auf `frontend/dist` und ist kein Mitglied der Gruppe
`zapclub`.

Infrastrukturdateien unter `deploy/` werden bei einem App-Release nicht als root
installiert. Änderungen daran separat validieren, explizit installieren und
danach Units, Backup, Monitor und öffentliche Endpunkte prüfen. Das vollständige
Runbook steht in `deploy/README.md`.

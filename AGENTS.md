# Zapclub – verbindlicher Projektkontext

## Quellen der Wahrheit

- Dieses Repository enthält ausschließlich Zapclub-Code, Tests,
  Dokumentation und projektspezifische Produktionsdateien.
- `tasks/concept.md` ist der lokale, nicht versionierte Produkt- und
  Architekturleitfaden. `tasks/plan.md` ist der lokale, lebende Entwicklungsplan.
  Beide vor nicht-trivialen Änderungen lesen und bei neuen Entscheidungen oder
  verändertem Ist-Stand im selben Arbeitsgang aktualisieren.
- `README.md` beschreibt Produkt und Architektur, `relay/README.md` die
  sicherheits- und protokollrelevanten Relay-Regeln und `deploy/README.md` das
  öffentliche Betriebsmodell.
- Für Frontend-Deployments ist `release-fast.sh` der Standard; `release.sh`
  mit vollständiger Verifikation nur auf ausdrücklichen Wunsch. Konkrete
  Laufzeitdefinitionen stehen in `deploy/`; flüchtige Betriebswerte werden hier
  nicht dupliziert.
- Bei Widersprüchen gilt das durch Tests belegte Verhalten des Codes. Betroffene
  Dokumentation muss im selben Stand nachgezogen werden.

## Architekturverträge

- Der Go-Relay ist die alleinige Autorität für gemeinsamen Club- und
  Wiedergabestatus. Browser konsumieren `now_playing`, korrigieren Drift und
  senden Absichten; sie wählen keinen eigenen Conductor.
- Es gibt pro Club genau drei gemeinsam belegte Stage-Slots. Echte DJs belegen
  sie über Stage-Events. Ein bewaffneter Auto-DJ ist ein dauerhaft sichtbarer
  virtueller Teilnehmer und belegt ebenfalls einen Slot; seine Playlist
  übernimmt die Wiedergabe nur, wenn kein echter DJ aktiv ist.
- Chat, Präsenz und Mitgliederliste sind nur für authentifizierte aktuelle
  Mitglieder lesbar. Öffentliche Mitglieder- und Hörerzahlen sind getrennte,
  relay-signierte Aggregate ohne Identitäten; individuelle anonyme Heartbeats
  bleiben serverseitig.
- Autoritative Wiedergabe-, Playlog-, Aggregat- und Credibility-Events dürfen
  ausschließlich vom Relay stammen. Rollen-, Zugriffs-, Rate- und Stage-Grenzen
  müssen relay-seitig durchgesetzt werden; UI-Sperren allein reichen nicht.
- NIP-29-Metadaten und `h`-getaggte Inhaltsarten werden in getrennten
  Subscriptions gelesen. Diese Grenze darf nicht zusammengelegt werden.
- Laufzeitdaten und Secrets liegen außerhalb unveränderlicher Releases und
  dürfen weder ins Repository noch in Build-Artefakte gelangen.

## Repository-Grenzen

- Zapclub-spezifische Caddy-Fragmente, Units, Backup-, Monitoring- und
  Smoke-Checks gehören in dieses Repository.
- Hostweite Firewall-, SSH-, Kernel-, Proxy-Importer- und Dispatcher-Logik sowie
  gemeinsame Infrastrukturverträge gehören in das Infrastruktur-Repository.
- Öffentliche Dokumentation beschreibt Architektur und Betriebsprinzipien,
  nicht Zielsysteme, Konten, Pfade, Zugangsdaten oder private Bedienabläufe.

## Arbeitsweise und Verifikation

- Vor Änderungen `tasks/concept.md`, `tasks/plan.md` und relevante Einträge in
  `tasks/lessons.md` prüfen und fremde Working-Tree-Änderungen bewahren.
- `tasks/plan.md` hält Ziele, Reihenfolge und Status über Aufgaben hinweg fest;
  `tasks/todo.md` bleibt die detaillierte Checkliste der aktuellen Arbeit.
  Produktideen ohne beschlossene Priorität bleiben als solche gekennzeichnet.
- Änderungen klein halten, am ursächlichen Modell ansetzen und passende
  Regressionstests ergänzen. Datenschutz- und Relay-Grenzen nicht nur im
  Frontend abbilden.
- Bei einem Deployment-Auftrag keine Audits, Tests, Typprüfungen, Vet- oder
  E2E-Läufe starten, sofern Joachim sie nicht ausdrücklich verlangt. Diese
  projektspezifische Vorgabe hat Vorrang vor allgemeinen Verifikationsregeln.
  Erforderliche Builds und die Kontrolle der tatsächlich aktivierten Revision
  bleiben Bestandteil des Deployments.
- Frontend-Releases aus sauberem lokalem `main` standardmäßig mit
  `ZAPCLUB_FAST_SKIP_CHECKS=1 ZAPCLUB_FAST_SKIP_AUDIT=1
  ZAPCLUB_FAST_SKIP_DEPS_INSTALL=1 ./release-fast.sh` ausführen.
  `./release.sh` nur verwenden, wenn die vollständigen Prüfungen ausdrücklich
  gewünscht sind. Keine direkten Änderungen im aktiven Release und kein
  `git pull` auf dem Server.
- Achtung: Der derzeitige serverseitige Deployment-Helfer führt eigene Tests
  und Checks aus; lokale Fast-Schalter deaktivieren diese nicht. Diesen
  verbleibenden Unterschied offen benennen, nicht als prüfungsfreien Deploy
  ausgeben. Hostweite Dispatcher-Anpassungen gehören ins Infrastruktur-Repository.
- Dateien unter `deploy/` werden nicht automatisch installiert. Werden sie
  geändert, sind Installation und anschließende Prüfung der betroffenen
  Dienste, Timer, Backups, Alarme und öffentlichen Endpunkte ein eigener
  Arbeitsschritt.

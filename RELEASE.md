# Release und Deployment

Stand: 6. September 2026.

## Einheitliche Release-CLI (Vertrag v1)

```sh
./release.sh --help       # Bedienung, keine Nebenwirkungen
./release.sh --dry-run    # lokaler Plan und Deploy-Voraussetzungen, kein Netzwerk
./release.sh --check      # lokales Prüfprofil, kein Push oder Deployment
./release.sh --deploy     # prüfen, exakten Commit übertragen, aktivieren, nachprüfen
```

Ohne Argument gilt `--deploy`. Genau einen Modus wählen. `--full` wählt das
vollständige Prüfprofil; in Projekten ohne Fast-Profil ist es gleichbedeutend
mit dem Standard. `--check` ist auch auf Entwicklungsbranches mit lokalen
Änderungen erlaubt. Es kann Abhängigkeiten installieren, Builds erzeugen und
projektspezifische Testdienste starten; es ist keine schreibfreie Vorschau.
Nur `--dry-run` bleibt ohne Checks und Serverzugriff. Es meldet mit Exitcode 1,
wenn Branch, Arbeitsbaum oder Projektvoraussetzungen einen Deploy verhindern.

Staging, Commit und Merge erfolgen separat und gezielt. Das Release-Skript
committet keine Dateien und akzeptiert keine Commit-Nachricht. Vor einem Deploy
muss `main` vollständig sauber sein. Der vor den Prüfungen ermittelte Commit
muss danach noch HEAD sein. Der Push adressiert diesen Commit exakt; Bundles
werden vor dem Upload auf dieselbe Revision geprüft.

Das Skript und `release-cli.sh` sind vollständig im eigenen Repository
versioniert; kein Import aus Sunnyhill und kein Download gemeinsamer Logik zur
Laufzeit. Der gemeinsame Vertrag wird in Sunnyhill koordiniert. Änderungen an
den lokalen Kopien mit denselben Offline-Vertragstests prüfen.

Ein später fehlgeschlagener lokaler Nachtest löst nicht automatisch den bereits
beendeten serverseitigen Rollback aus. Dann die aktive Revision prüfen und das
projektspezifische Rollback-Verfahren anwenden. App-Release, Installation von
Deploy-Helfern/Units/Caddy und Updates externer Dienste bleiben getrennte Vorgänge.

Standardprofil: **fast**, entsprechend der bisherigen Deploy-Präferenz. Die drei `ZAPCLUB_FAST_SKIP_*`-Schalter stehen standardmäßig auf `1`; bei fehlenden Abhängigkeiten wird weiterhin installiert. `./release.sh --check --full` beziehungsweise `--deploy --full` wählen ausdrücklich das volle Frontend-/Go-/E2E-Profil. `release-fast.sh` ist ein Kompatibilitätseinstieg zum gleichen Skript. Der Server prüft und baut weiterhin den gesamten Stack und startet Relay, Bot und LNURL neu. Details: [Betriebsmodell](deploy/README.md).

Offline-CLI-Prüfung: `python3 deploy/test-release-cli.py`.

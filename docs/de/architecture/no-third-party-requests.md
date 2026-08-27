# Keine Drittanbieter-Anfragen

Die Projektregel *„keine Cloud-Abhängigkeiten für Kernfunktionen"* gilt
auch für die Dokumentations-Site. Wer diese Seiten liest, soll seine
IP-Adresse nicht an jemanden ausliefern, den wir nicht gefragt haben.

Das ist nicht die Voreinstellung. Zensical wendet sich ab Werk an zwei
externe Dienste, genau wie Material for MkDocs davor.

## Mermaid von unpkg.com

Zensical rendert Mermaid-Diagramme im Browser und lädt den Renderer beim
ersten Diagramm nach. Sein Loader sieht so aus:

```js
typeof mermaid == "undefined" || mermaid instanceof Element
  ? load("https://unpkg.com/mermaid@11/dist/mermaid.min.js")
  : /* schon da, nichts tun */
```

Die Abfrage ist der Ansatzpunkt. Das `mermaid.min.js` von upstream ist
ein einzelnes, in sich geschlossenes Bündel ohne dynamische Importe,
dessen letzte Anweisung lautet:

```js
globalThis["mermaid"] = globalThis.__esbuild_esm_mermaid_nm["mermaid"].default;
```

Laden wir dieselbe Datei also selbst, bevor Zensical sie braucht, ist die
globale Variable bereits definiert, die Abfrage greift nicht, und die
CDN-Anfrage findet nie statt. Kein Wrapper-Modul, kein gepatchtes Theme.

`.github/scripts/vendor-mermaid.sh` lädt eine gepinnte Version, prüft sie
gegen eine hinterlegte SHA-384-Summe und schreibt sie nach
`docs/en/javascripts/` und `docs/de/javascripts/`. Beide Konfigurationen
verweisen dann darauf:

```toml
extra_javascript = ["javascripts/mermaid.min.js"]
```

Die Datei ist rund 3,5 MB groß und wird **nicht** eingecheckt — sie ist
ein reproduzierbares Build-Artefakt, das während des Builds geholt und
von unserem eigenen Ursprung ausgeliefert wird. Ein übersprungener
Vendoring-Schritt fiele stillschweigend auf das CDN zurück, deshalb prüft
die CI, dass die Datei in beiden Ausgaben vorhanden ist.

Upstream schlägt
[zensical/backlog#155](https://github.com/zensical/backlog/issues/155)
vor, Diagramme zur Build-Zeit zu rendern. Das würde das 3,5-MB-Bündel im
Browser vollständig überflüssig machen und diesen Abschnitt gleich mit.

## ESP Web Tools von unpkg.com

Die Installationsseite für das
[CYD-Scan-Panel](../hardware/cyd-scan-panel.md) bindet
[ESP Web Tools](https://esphome.github.io/esp-web-tools/) ein — eine
Web-Komponente, die ESP32-Firmware über Web Serial direkt aus dem Browser
flasht. Jedes veröffentlichte Beispiel lädt sie von unpkg.com:

```html
<script type="module" src="https://unpkg.com/esp-web-tools@10/dist/web/install-button.js?module"></script>
```

Anders als bei Mermaid ist das kein geschlossenes Bündel: Der
Einstiegspunkt importiert dynamisch einen gemeinsamen Dialog-/Konsolen-Teil
und je einen Flasher-Stub pro Chipfamilie, alle als Pfade relativ zur
eigenen URL (gegen den veröffentlichten Paketinhalt geprüft — nirgends im
Bündel steht eine absolute CDN-URL). Würden wir nur `install-button.js`
selbst ausliefern, zeigten diese relativen Importe zwar auf **unseren**
Ursprung, aber auf Dateien, die dort nicht existieren. Das ganze
Verzeichnis `dist/web/` muss gemeinsam übernommen werden, sonst laufen
die dynamischen Importe in 404er.

`.github/scripts/vendor-esp-web-tools.sh` lädt ein gepinntes npm-Paket,
prüft das Tarball gegen die SHA-512-Integritätssumme, die die Registry
für genau diese Version selbst veröffentlicht, und schreibt jede Datei
aus `dist/web/` nach `docs/en/javascripts/esp-web-tools/`. Die
Installationsseite verweist direkt darauf:

```html
<script src="/en/javascripts/panel-tools.js"></script>
```

Dieselbe Abwägung wie bei Mermaid: rund 540 KB, nicht eingecheckt (ein
reproduzierbares Build-Artefakt, während des Builds geholt und vom
eigenen Ursprung ausgeliefert), und die CI-Prüfung auf externe Assets
finge auch hier einen Rückfall ab — ein wurzelrelatives `src` ist schon
per Konstruktion gleicher Ursprung, es gibt also nichts freizugeben.

## Scalar von proxy.scalar.com und fonts.scalar.com

Die [API-Referenz](/en/api-reference/) bindet
[Scalar](https://github.com/scalar/scalar) ein, um
`components/scan-bridge/api/openapi.yaml` interaktiv darzustellen. Ihr
Bündel `dist/browser/standalone.js` ist in sich geschlossen — anders als
ESP Web Tools hat es keine dynamischen Importe, die standardmäßig gegen
eine absolute CDN-URL auflösen (gegen den veröffentlichten Paketinhalt
geprüft, als `.github/scripts/vendor-scalar.sh` entstand). Das Bündel
selbst ruft im Browser aber zwei Dienste Dritter auf, beide über die
eigene Konfiguration abschaltbar:

- Das Standard-Theme lädt Schriften von `https://fonts.scalar.com`.
- Der CORS-Umweg des „Try it"-Bereichs schickt Live-Anfragen über
  `https://proxy.scalar.com`.

Beides ist dort abgeschaltet, wo die Seite `Scalar.createApiReference()`
aufruft:

```js
Scalar.createApiReference('#api-reference', {
  url: 'openapi.yaml',
  proxyUrl: '',
  withDefaultFonts: false,
})
```

`.github/scripts/vendor-scalar.sh` lädt das gepinnte npm-Paket (eine
Einzeldatei-URL wie bei Mermaid auf unpkg gibt es dafür nicht), prüft das
Tarball gegen die von der Registry veröffentlichte
SHA-512-Integritätssumme und entpackt ausschließlich
`dist/browser/standalone.js` nach
`docs/en/javascripts/scalar/standalone.js` — nur englisch, eine deutsche
API-Referenz gibt es nicht. Rund 3,6 MB, nicht eingecheckt (ein
reproduzierbares Build-Artefakt, während des Builds geholt und vom
eigenen Ursprung ausgeliefert).

Anders als bei Mermaid und ESP Web Tools ist das kein Ladevorgang, den
`check_no_external_assets.py` übersehen und dann per Vorhandenseinsprüfung
abfangen könnte: `proxy.scalar.com` und `fonts.scalar.com` tauchen gar
nicht erst als `<script src>`- oder `<link href>`-Markup auf, und fiele
die obige Konfiguration je weg, wären die entstehenden Anfragen
JavaScript-initiiert — unsichtbar für die statische HTML-Analyse dieses
Skripts, derselbe blinde Fleck wie beim GitHub-Aufruf weiter unten. Die
Konfiguration ist das Einzige, was zwischen dieser Seite und beiden
Dritten steht; für die CI gibt es hier nichts zu prüfen.

## Google Fonts

Das Standard-Theme lädt Inter und JetBrains Mono von
`fonts.googleapis.com`, mit einem `preconnect` auf `fonts.gstatic.com`.
Abgeschaltet:

```toml
[project.theme]
font = false
```

Die Site nutzt stattdessen Systemschriften, und die CI lässt den Build
scheitern, sobald ein Font-Link wieder auftaucht.

## Der eine, der geblieben ist

Ein gesetztes `repo_url` bringt das Theme dazu,
`https://api.github.com/repos/<owner>/<repo>` und `.../releases/latest`
abzurufen, um Version und Sternezahl im Kopf anzuzeigen. Das Ergebnis
liegt im `sessionStorage`, es sind also ein bis zwei Anfragen je
Besuchssitzung statt einer pro Seite — mit frischem Browserprofil
gemessen feuert sie auf der ersten englischen Seite und erneut auf der
ersten deutschen. Es bleibt eine Anfrage an Dritte, und Zensical bietet
keinen Schalter dafür: Der Abruf hängt allein daran, dass `repo_url` auf
eine GitHub-URL passt.

Abschalten hieße, `repo_url` zu entfernen — womit auch der
Repository-Link im Kopf und die Schaltfläche „Diese Seite bearbeiten"
verschwänden. Diese Abwägung ist noch nicht getroffen.

## Nachprüfen

Site bauen, lokal ausliefern und das Netzwerkprotokoll mit leerem Cache
und geleertem Speicher beobachten. Externe Anfragen sollten es genau
keine geben:

```bash
make test-docs
python3 -m http.server 8765 --directory site
```

Dann `/`, `/de/architecture/` und `/en/` laden und prüfen, dass nichts
außerhalb von `127.0.0.1` angefragt wird — mit Ausnahme des oben
beschriebenen GitHub-Aufrufs.

`make test-docs` führt `.github/scripts/check_no_external_assets.py` aus.
Das Skript liest jede erzeugte Seite und lässt den Build scheitern, sobald
ein `<script src>`, `<img src>`, `<iframe src>` oder ein ladendes `<link>`
auf einen fremden Ursprung zeigt. JavaScript-initiierte Anfragen sieht es
nicht — genau deshalb braucht der GitHub-Aufruf den Hinweis oben.

package config

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/strausmann/fileee-mcp-server/internal/diag"
	"github.com/strausmann/gangway/origin"
)

// Env liest eine Umgebungsvariable. Der Umweg ueber diesen Typ statt ueber
// os.Getenv haelt LoadConfig frei von globalem Zustand und macht die
// Konfiguration ohne t.Setenv parallel testbar.
type Env func(key string) string

// AuthMode bestimmt, wie sich Clients gegenueber diesem Server ausweisen.
type AuthMode string

// Die drei Authentifizierungs-Modi.
const (
	// AuthOIDC prueft Bearer-Tokens eines externen Identity Providers.
	AuthOIDC AuthMode = "oidc"
	// AuthToken prueft ein statisches Bearer-Token aus der Konfiguration.
	AuthToken AuthMode = "token"
	// AuthBoth erlaubt beides; der JWT-Pfad hat Vorrang.
	AuthBoth AuthMode = "both"
)

// AccountMode bestimmt, ob der Server ein oder mehrere Fileee-Konten bedient.
type AccountMode string

// Die zwei Konto-Modi.
const (
	// ModeSingle bedient genau ein Konto aus FILEEE_USERNAME/_PASSWORD.
	ModeSingle AccountMode = "single"
	// ModeMulti bildet Token-Subjects auf mehrere Konten ab.
	ModeMulti AccountMode = "multi"
)

// defaultAccountKey ist der Konto-Key im single-Modus. Der Pool behandelt
// beide Modi damit identisch — single ist ein Pool mit genau einem Eintrag.
const defaultAccountKey = "default"

// maxInstanceDescriptionRunes begrenzt MCP_INSTANCE_DESCRIPTION. Die Grenze
// dient nicht der Sicherheit — der Wert ist Betreiber-Konfiguration —, sondern
// verhindert, dass eine versehentlich falsch belegte Variable (etwa ein
// hineinkopierter Dateiinhalt) den Kontext jeder Sitzung flutet, ohne beim
// Start aufzufallen. Gezählt werden ZEICHEN, nicht Bytes.
const maxInstanceDescriptionRunes = 2000

// accountKeyMuster begrenzt Konto-Keys auf Zeichen, die als Dateiname sicher
// sind. Ohne diese Pruefung waere ein Key wie "../../etc/x" ein Schreibzugriff
// ausserhalb des Session-Verzeichnisses.
var accountKeyMuster = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Account beschreibt ein Fileee-Konto samt der Identitaeten, die darauf zeigen.
type Account struct {
	// Key ist der interne Bezeichner, zugleich Name der Session-Datei.
	Key string
	// Username, Password und TOTPSeed sind die Fileee-Zugangsdaten.
	Username string
	Password string
	TOTPSeed string
	// Subjects sind die Claim-Werte, die auf dieses Konto abbilden.
	Subjects []string
}

// OIDCProvider waehlt den Identity Provider. Jeder Wert hat einen eigenen
// Variablen-Namensraum — die Anforderungen der Anbieter werden bewusst NICHT
// vermischt, damit jede Anleitung nur ihre eigenen Variablen nennt.
type OIDCProvider string

// Die drei unterstuetzten Anbieter-Zweige.
const (
	// ProviderEntra leitet den Aussteller aus der Entra-Verzeichnis-ID ab.
	ProviderEntra OIDCProvider = "entra"
	// ProviderAuthentik leitet den Aussteller aus Authentik-Host und
	// Anwendungs-Kuerzel ab.
	ProviderAuthentik OIDCProvider = "authentik"
	// ProviderGeneric nimmt Aussteller und Client-ID direkt entgegen und
	// bedient damit jeden standardkonformen OpenID-Connect-Anbieter ohne
	// eigenen Zweig — etwa GitLab oder Keycloak.
	ProviderGeneric OIDCProvider = "generic"
)

// Config buendelt die gesamte Laufzeitkonfiguration. Sie entsteht ausschliesslich
// in LoadConfig — keine andere Stelle im Server liest Umgebungsvariablen.
type Config struct {
	AuthMode           AuthMode
	OIDCProvider       OIDCProvider
	OIDCIssuer         string
	OIDCClientID       string
	OIDCSubjectClaim   string
	OIDCRequiredScopes []string
	// OIDCAdvertisedScopes wird, wenn gesetzt, statt OIDCRequiredScopes VOR
	// jedem Token-Austausch angekuendigt (WWW-Authenticate "scope"-Parameter
	// und RFC-9728 scopes_supported, siehe internal/server/server.go,
	// gangway.serve.Config.AdvertisedScopes ab v0.5.0) -- OIDCRequiredScopes
	// selbst bleibt unveraendert das, wogegen scopesSatisfied den Token-Claim
	// prueft (scopes.go). Beide Werte sind bei den meisten Anbietern
	// identisch und diese Variable bleibt leer; Entra ist die dokumentierte
	// Ausnahme: ein nackter Scope-Name wie "mcp.access" wird beim
	// Token-Austausch mit AADSTS650053 abgelehnt ("scope ... that doesn't
	// exist on the resource 'Microsoft Graph'"), angekuendigt werden muss
	// dort die vollqualifizierte Form, waehrend der scp-Claim im
	// ausgestellten Token weiterhin nur den kurzen Namen traegt.
	OIDCAdvertisedScopes []string
	ResourceURL          string
	APIToken             string
	AllowedSubjects      []string

	AccountMode AccountMode
	Accounts    []Account

	// InstanceDescription beschreibt in Prosa, welche Umgebung und welches
	// Fileee-Konto diese Instanz bedient — etwa "Testumgebung, Wegwerfdaten"
	// gegenüber "produktives Archiv, echte Verträge". Der Wert wird über das
	// instructions-Feld der initialize-Antwort und über whoami ausgespielt.
	//
	// Er stammt aus der Betreiber-Konfiguration (Infisical), nie aus einer
	// Aufrufereingabe und nie aus einem Dokumentinhalt — deshalb ist er keine
	// Fläche für eingeschleuste Anweisungen.
	//
	// Leer bedeutet: kein instructions-Feld, kein whoami-Feld. Der Server
	// verhält sich dann exakt wie vor Einführung dieser Variablen.
	InstanceDescription string

	// MaxUploadBytes wird seit dem write-tools-Increment (Task 5,
	// internal/tools/write_documents.go, uploadDocumentHandler) tatsaechlich
	// durchgesetzt: upload_document lehnt einen Aufruf ab, sobald entweder
	// die aus der base64-kodierten Laenge konservativ hochgerechnete
	// Obergrenze oder die tatsaechlich dekodierte Bytezahl von
	// contentBase64 diesen Wert ueberschreitet — bevor go-fileees
	// DocumentService.Upload(ctx, r io.Reader, meta UploadMetadata) je
	// aufgerufen wird. Eine fruehere Fassung dieses Kommentars behauptete,
	// es gebe fuer diesen Wert noch KEINE Aufrufstelle — das galt bis zu
	// diesem Increment und ist jetzt ueberholt.
	//
	// MaxDownloadBytes ist davon UNABHAENGIG weiterhin unverdrahtet: die
	// beiden binaerliefernden Lese-Werkzeuge, die es heute bereits gibt
	// (get_document_pdf/get_page_image, internal/tools/read_binary.go,
	// DocumentService.DownloadPDF/DownloadPageImage), lesen diesen Wert
	// NICHT — sie erzwingen ihre eigene, fest verdrahtete 8-MiB-Obergrenze
	// (maxBinaryBytes, read_binary.go) unabhaengig von jeder Konfiguration.
	// Ihr natuerlicher Ort fuer MaxDownloadBytes waere absehbar derselbe wie
	// bei MaxUploadBytes: um den io.ReadCloser herum, den die jeweilige
	// Methode zurueckliefert (ein limitierender io.ReadCloser-Wrapper) —
	// NICHT die HTTP-Ebene dieses Servers. Das ist die Aufgabe eines
	// kuenftigen Umbaus dieser beiden Werkzeuge auf einen konfigurierbaren
	// statt fest verdrahteten Grenzwert, nicht dieser Konfiguration.
	//
	// MaxRequestBodyBytes ist davon unabhaengig abgeleitet
	// (ladeZahlenwerte) und WUERDE den 4-MiB-Default des MCP-SDK
	// ueberschreiben, sobald ein Aufrufer sie liest — aber Gangway v0.2.0
	// baut den Streamable-HTTP-Handler intern (serve.AttachMCP/
	// AttachMCPSelector) mit einem fest verdrahteten
	// &mcp.StreamableHTTPOptions{Stateless: true} und bietet keinen Weg,
	// dessen MaxRequestBodyBytes-Feld zu setzen. Dieser dritte Wert bleibt
	// deshalb aus einem GANZ ANDEREN, unabhaengigen Grund unverdrahtet als
	// die beiden obigen — nicht wegen einer fehlenden Aufrufstelle,
	// sondern wegen einer fehlenden Erweiterungsmoeglichkeit in Gangway.
	// Kandidat fuer eine Gangway-Erweiterung (siehe Nachtrag zu ADR-0015).
	MaxDownloadBytes    int64
	MaxUploadBytes      int64
	MaxRequestBodyBytes int64
	// MaxInflight, RateRPS/RateBurst und RateGlobalRPS/RateGlobalBurst
	// werden von internal/server.newToolCallLimiter durchgesetzt — siehe
	// dort (internal/server/ratelimit.go) fuer die Bedeutung jedes einzelnen
	// Werts und die Begruendung, warum RateRPS/RateBurst am verifizierten
	// Token-Subject haengen, nicht an der Client-Adresse.
	MaxInflight int

	ListenAddr        string
	SessionDir        string
	KeepaliveInterval time.Duration
	RateRPS           float64
	RateBurst         int
	RateGlobalRPS     float64
	RateGlobalBurst   int

	// TrustedProxies sind die Netze, deren Weiterleitungs-Header (siehe
	// ClientIPHeaderMode) als tatsaechliche Client-Adresse geglaubt werden.
	// Leer bedeutet: kein Proxy davor, es zaehlt nur die Peer-Adresse.
	TrustedProxies []netip.Prefix
	// AllowedOriginPrefixes ist die Adress-Freigabeliste, die Gangway vor
	// jeden Zugriff auf /mcp schaltet (siehe ADR-0015) — ohne sie startet
	// New im Modus oidc gar nicht erst, ein Server ohne Filter darf nicht
	// hochkommen.
	AllowedOriginPrefixes []netip.Prefix
	// ClientIPHeaderMode waehlt den EINEN Weiterleitungs-Header, der als
	// Quelle der Client-Adresse gilt (github.com/strausmann/gangway/origin).
	// Gangway wertet bewusst nicht mehrere Header der Reihe nach aus — das
	// wuerde einem Aufrufer erlauben, sich den Header auszusuchen, der dem
	// Server gerade passt. Default cf-connecting-ip folgt der bisherigen
	// Priorisierung dieses Servers; er MUSS gegen die tatsaechliche
	// Proxy-Kette (Pangolin/Traefik vs. direktes Cloudflare-Terminieren)
	// geprueft werden, bevor TrustedProxies produktiv gesetzt wird.
	ClientIPHeaderMode origin.HeaderMode

	// LogLevel waehlt die Stufe des diagnostischen Loggers, den
	// internal/server.New ueber internal/diag.New baut (siehe dessen
	// Paket-Kommentar): "info" (Vorgabe) protokolliert Werkzeugname,
	// Dauer, Ergebnisart und die aufgeloeste Faehigkeitsmenge — "debug"
	// zusaetzlich die vom Aufrufer uebergebenen Werkzeug-Argumente. Beide
	// Stufen laufen durch dieselbe Maskierung (internal/diag), die jedes
	// Feld mit einem verdaechtigen Namen unabhaengig von der Stufe
	// ersetzt. Derselbe Logger wird auch an go-fileee durchgereicht
	// (fileee.WithLogger, siehe internal/server.New) — dessen eigenes
	// Debug-Protokoll (Methode, Pfad, Status je HTTP-Versuch) landet damit
	// automatisch hinter derselben Maskierung.
	//
	// Vor dieser Aenderung wurde die Variable zwar geladen, hatte aber
	// keinen Konsumenten — Start-/Fehlermeldungen in
	// cmd/fileee-mcp-server/main.go liefen (und laufen weiterhin) ueber
	// schlichtes fmt.Fprintf auf stdout/stderr, unabhaengig von dieser
	// Einstellung.
	LogLevel diag.Level

	// IssuedIDTTLSeconds und IssuedIDMaxPerIdentity steuern
	// internal/issued.Store — die Merkliste, die je verifizierter Identität
	// festhält, welche Dokument-/Kontakt-/Reminder-IDs ein Lese-Werkzeug
	// dieses Servers tatsächlich ausgeliefert hat (ADR-0013 Punkt 3, siehe
	// dessen Paket-Doc-Kommentar). internal/server.New baut daraus den Store
	// (issued.New(time.Duration(IssuedIDTTLSeconds)*time.Second,
	// int(IssuedIDMaxPerIdentity))) — beide Werte werden dort tatsächlich
	// ausgewertet (Store.ttl/Store.maxPerIdentity), nicht nur geladen und
	// liegengelassen. Das war bei FILEEE_MAX_UPLOAD_BYTES vor dem
	// write-tools-Increment (siehe dessen eigenen Doc-Kommentar oben) genau
	// der Fehler: eine Einstellung, die LoadConfig zwar entgegennahm, die
	// aber keine Aufrufstelle je las — ein Betreiber, der sie setzte, bekam
	// unbemerkt das Verhalten des unveränderten Defaults.
	//
	// Beide Grenzfälle <= 0 werden bewusst NICHT als "unbegrenzt" gelesen,
	// sondern als reale, durchgesetzte Grenze (Store.ttl/Store.maxPerIdentity
	// eigene Doc-Kommentare) — dieselbe fail-closed-Konvention wie bei
	// FILEEE_MAX_INFLIGHT. intWert weist nur NEGATIVE Werte ab; 0 ist ein
	// gültiger, durchgesetzter Wert ("sofort verfallen" bzw. "nichts wird
	// gemerkt"), keine Fehlkonfiguration.
	//
	// IssuedIDTTLSeconds begrenzt, wie lange eine aufgenommene ID gültig
	// bleibt (Default 1800 Sekunden = 30 Minuten).
	IssuedIDTTLSeconds int64
	// IssuedIDMaxPerIdentity begrenzt, wie viele IDs der Eimer einer
	// einzelnen Identität gleichzeitig halten darf (Default 1000).
	IssuedIDMaxPerIdentity int64

	// Warnings sind Hinweise, die den Start nicht verhindern, aber beim Boot
	// protokolliert werden sollen.
	Warnings []string

	subjectIndex map[string]string
}

// AccountBySubject loest einen Claim-Wert auf einen Konto-Key auf. Ein
// unbekanntes Subject liefert bewusst kein Ergebnis — es gibt keinen Fallback
// auf ein Standardkonto.
func (c *Config) AccountBySubject(subject string) (string, bool) {
	key, ok := c.subjectIndex[subject]
	return key, ok
}

// LoadConfig liest die vollstaendige Konfiguration und validiert sie
// vollstaendig, bevor der Server startet. Jede Verletzung ist ein Abbruch mit
// einer Meldung, die die betroffene Variable benennt.
func LoadConfig(env Env) (*Config, error) {
	cfg := &Config{
		AuthMode:     AuthMode(orDefault(env("MCP_AUTH_MODE"), string(AuthToken))),
		OIDCProvider: OIDCProvider(strings.TrimSpace(env("MCP_OIDC_PROVIDER"))),
		// Der Vorgabewert haengt vom Anbieter ab und wird deshalb erst in
		// resolveProvider gesetzt — hier steht nur die ausdrueckliche Angabe.
		OIDCSubjectClaim:     strings.TrimSpace(env("MCP_OIDC_SUBJECT_CLAIM")),
		OIDCRequiredScopes:   splitListe(env("MCP_OIDC_REQUIRED_SCOPES")),
		OIDCAdvertisedScopes: splitListe(env("MCP_OIDC_ADVERTISED_SCOPES")),
		ResourceURL:          strings.TrimSpace(env("MCP_RESOURCE_URL")),
		APIToken:             env("MCP_API_TOKEN"),
		AllowedSubjects:      splitListe(env("MCP_ALLOWED_SUBJECTS")),
		AccountMode:          AccountMode(orDefault(env("FILEEE_MODE"), string(ModeSingle))),
		ListenAddr:           orDefault(env("MCP_LISTEN_ADDR"), ":8080"),
		SessionDir:           orDefault(env("FILEEE_SESSION_DIR"), "/home/nonroot/sessions"),
		ClientIPHeaderMode:   origin.HeaderMode(orDefault(env("FILEEE_CLIENT_IP_HEADER_MODE"), string(origin.ModeCFConnectingIP))),
		LogLevel:             diag.Level(orDefault(env("FILEEE_LOG_LEVEL"), string(diag.LevelInfo))),
		InstanceDescription:  strings.TrimSpace(env("MCP_INSTANCE_DESCRIPTION")),
	}

	switch cfg.ClientIPHeaderMode {
	case origin.ModeXForwardedFor, origin.ModeXRealIP, origin.ModeCFConnectingIP:
	default:
		return nil, fmt.Errorf("FILEEE_CLIENT_IP_HEADER_MODE = %q — erlaubt sind %s, %s, %s",
			cfg.ClientIPHeaderMode, origin.ModeXForwardedFor, origin.ModeXRealIP, origin.ModeCFConnectingIP)
	}
	switch cfg.LogLevel {
	case diag.LevelInfo, diag.LevelDebug:
	default:
		return nil, fmt.Errorf("FILEEE_LOG_LEVEL = %q — erlaubt sind %q, %q",
			cfg.LogLevel, diag.LevelInfo, diag.LevelDebug)
	}

	if n := utf8.RuneCountInString(cfg.InstanceDescription); n > maxInstanceDescriptionRunes {
		return nil, fmt.Errorf("MCP_INSTANCE_DESCRIPTION ist %d Zeichen lang — erlaubt sind höchstens %d",
			n, maxInstanceDescriptionRunes)
	}

	switch cfg.AuthMode {
	case AuthOIDC, AuthToken, AuthBoth:
	default:
		return nil, fmt.Errorf("MCP_AUTH_MODE = %q — erlaubt sind oidc, token, both", cfg.AuthMode)
	}
	switch cfg.AccountMode {
	case ModeSingle, ModeMulti:
	default:
		return nil, fmt.Errorf("FILEEE_MODE = %q — erlaubt sind single, multi", cfg.AccountMode)
	}

	if err := ladeZahlenwerte(cfg, env); err != nil {
		return nil, err
	}
	if err := ladeNetzwerk(cfg, env); err != nil {
		return nil, err
	}
	if err := ladeAuth(cfg, env); err != nil {
		return nil, err
	}
	if err := ladeKonten(cfg, env); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ladeNetzwerk liest die beiden IP-Praefixlisten. Beide werden hier und nicht
// erst beim Bau des Gangway-Unterbaus geparst — ein unbrauchbares Praefix
// soll den Start mit einer benannten Variable abbrechen, nicht irgendwo tief
// in einer fremden Bibliothek.
func ladeNetzwerk(cfg *Config, env Env) error {
	var err error
	if cfg.TrustedProxies, err = praefixListe(env, "FILEEE_TRUSTED_PROXIES"); err != nil {
		return err
	}
	if cfg.AllowedOriginPrefixes, err = praefixListe(env, "FILEEE_ALLOWED_ORIGIN_PREFIXES"); err != nil {
		return err
	}
	return nil
}

// praefixListe liest eine kommaseparierte Liste aus CIDR-Praefixen oder
// einzelnen IP-Adressen. Eine einzelne Adresse wird als Praefix mit voller
// Bitlaenge behandelt (/32 bei IPv4, /128 bei IPv6) — wer eine einzelne
// Maschine meint, tippt selten eine Maske dazu, und TestLoadConfigListenUndZahlenwerte
// aus Aufgabe 1 verlangt genau das bereits fuer FILEEE_TRUSTED_PROXIES.
func praefixListe(env Env, key string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, teil := range splitListe(env(key)) {
		if p, err := netip.ParsePrefix(teil); err == nil {
			out = append(out, p)
			continue
		}
		addr, err := netip.ParseAddr(teil)
		if err != nil {
			return nil, fmt.Errorf("%s: %q ist weder eine IP-Adresse noch ein CIDR-Praefix", key, teil)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func ladeZahlenwerte(cfg *Config, env Env) error {
	var err error
	if cfg.MaxDownloadBytes, err = intWert(env, "FILEEE_MAX_DOWNLOAD_BYTES", 1<<20); err != nil {
		return err
	}
	if cfg.MaxUploadBytes, err = intWert(env, "FILEEE_MAX_UPLOAD_BYTES", 2<<20); err != nil {
		return err
	}
	inflight, err := intWert(env, "FILEEE_MAX_INFLIGHT", 8)
	if err != nil {
		return err
	}
	cfg.MaxInflight = int(inflight)

	burst, err := intWert(env, "FILEEE_RATE_BURST", 3)
	if err != nil {
		return err
	}
	cfg.RateBurst = int(burst)
	globalBurst, err := intWert(env, "FILEEE_RATE_GLOBAL_BURST", 3)
	if err != nil {
		return err
	}
	cfg.RateGlobalBurst = int(globalBurst)

	if cfg.RateRPS, err = floatWert(env, "FILEEE_RATE_RPS", 1); err != nil {
		return err
	}
	if cfg.RateGlobalRPS, err = floatWert(env, "FILEEE_RATE_GLOBAL_RPS", 1); err != nil {
		return err
	}
	if cfg.KeepaliveInterval, err = dauerWert(env, "FILEEE_KEEPALIVE_INTERVAL", 15*time.Minute); err != nil {
		return err
	}

	// Siehe Config.IssuedIDTTLSeconds/IssuedIDMaxPerIdentity für die
	// Bedeutung und internal/server.New für die Aufrufstelle, die diese
	// Werte tatsächlich auswertet.
	if cfg.IssuedIDTTLSeconds, err = intWert(env, "FILEEE_ISSUED_ID_TTL_SECONDS", 1800); err != nil {
		return err
	}
	if cfg.IssuedIDMaxPerIdentity, err = intWert(env, "FILEEE_ISSUED_ID_MAX_PER_IDENTITY", 1000); err != nil {
		return err
	}

	// Base64 blaeht den Nutzinhalt um Faktor 4/3 auf, dazu kommt der
	// JSON-RPC-Rahmen. Ohne diese Ableitung wuerde der 4-MiB-Default des SDK
	// groessere Uploads mit 413 abweisen, bevor der Tool-Handler laeuft.
	// maxRequestBodyBytesFor saettigt statt zu ueberlaufen, wenn
	// FILEEE_MAX_UPLOAD_BYTES sehr gross gewaehlt ist (deren eigener
	// Doc-Kommentar erklaert, warum genau dieselbe Ueberlaufklasse hier
	// wie in internal/tools/write_documents.go's base64EncodedLenFor
	// auftreten kann).
	cfg.MaxRequestBodyBytes = maxRequestBodyBytesFor(cfg.MaxUploadBytes)
	return nil
}

// guidPattern beschreibt die Mandanten-Kennung, wie Entra sie in der
// Portal-Uebersicht als „Verzeichnis-ID (Mandant)" anzeigt.
var guidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// providerVariables listet je Anbieter die Variablen, die ausschliesslich ihm
// gehoeren. Daraus entstehen sowohl die Pflichtpruefung als auch die Meldung,
// wenn jemand Variablen zweier Anbieter mischt.
var providerVariables = map[OIDCProvider][]string{
	ProviderEntra:     {"MCP_ENTRA_TENANT_ID", "MCP_ENTRA_CLIENT_ID"},
	ProviderAuthentik: {"MCP_AUTHENTIK_BASE_URL", "MCP_AUTHENTIK_APP_SLUG", "MCP_AUTHENTIK_CLIENT_ID"},
	ProviderGeneric:   {"MCP_OIDC_ISSUER", "MCP_OIDC_CLIENT_ID"},
}

// resolveProvider fuellt OIDCIssuer und OIDCClientID aus den Variablen des
// gewaehlten Anbieters. Jeder Anbieter hat einen eigenen Variablen-Namensraum:
// Ein Betreiber, der die Entra-Anleitung liest, begegnet nie einer
// Authentik-Variablen und umgekehrt.
func resolveProvider(cfg *Config, env Env) error {
	if cfg.OIDCProvider == "" {
		return fmt.Errorf("MCP_OIDC_PROVIDER ist im Modus %q Pflicht — erlaubt sind %q, %q, %q",
			cfg.AuthMode, ProviderEntra, ProviderAuthentik, ProviderGeneric)
	}
	if _, ok := providerVariables[cfg.OIDCProvider]; !ok {
		return fmt.Errorf("MCP_OIDC_PROVIDER = %q — erlaubt sind %q, %q, %q",
			cfg.OIDCProvider, ProviderEntra, ProviderAuthentik, ProviderGeneric)
	}
	if err := rejectForeignProviderVariables(cfg.OIDCProvider, env); err != nil {
		return err
	}

	// Der sinnvolle Subject-Claim folgt aus dem Anbieter, deshalb setzt ihn der
	// Anbieter — nicht der Betreiber. Bei Entra ist `sub` paarweise
	// pseudonymisiert und im Portal nirgends ablesbar; ablesbar (und
	// mandantenweit stabil) ist `oid`. Eine ausdrueckliche Angabe schlaegt den
	// Vorgabewert immer.
	if cfg.OIDCSubjectClaim == "" {
		cfg.OIDCSubjectClaim = defaultSubjectClaim(cfg.OIDCProvider)
	}

	switch cfg.OIDCProvider {
	case ProviderEntra:
		return resolveEntra(cfg, env)
	case ProviderAuthentik:
		return resolveAuthentik(cfg, env)
	default:
		return resolveGeneric(cfg, env)
	}
}

// defaultSubjectClaim liefert den Claim, der beim jeweiligen Anbieter die
// brauchbare Identitaet traegt.
func defaultSubjectClaim(provider OIDCProvider) string {
	if provider == ProviderEntra {
		return "oid"
	}
	return "sub"
}

// rejectForeignProviderVariables bricht ab, wenn Variablen eines anderen
// Anbieters gesetzt sind. Ohne diese Pruefung wuerden sie stillschweigend
// ignoriert — der Betreiber sucht dann den Fehler an einer Einstellung, die
// gar nicht gelesen wird.
func rejectForeignProviderVariables(active OIDCProvider, env Env) error {
	var stray []string
	for provider, names := range providerVariables {
		if provider == active {
			continue
		}
		for _, name := range names {
			if strings.TrimSpace(env(name)) != "" {
				stray = append(stray, name)
			}
		}
	}
	if len(stray) == 0 {
		return nil
	}
	sort.Strings(stray)
	return fmt.Errorf("MCP_OIDC_PROVIDER = %q, aber gesetzt sind auch Variablen anderer "+
		"Anbieter: %s — diese werden nicht gelesen. Entweder entfernen oder den "+
		"passenden Anbieter waehlen", active, strings.Join(stray, ", "))
}

// rejectOIDCVariables bricht ab, wenn im reinen token-Modus Anbieter-Variablen
// gesetzt sind — das Gegenstueck zu rejectForeignProviderVariables, eine Ebene
// hoeher.
func rejectOIDCVariables(mode AuthMode, env Env) error {
	names := []string{"MCP_OIDC_PROVIDER"}
	for _, group := range providerVariables {
		names = append(names, group...)
	}

	var set []string
	for _, name := range names {
		if strings.TrimSpace(env(name)) != "" {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return nil
	}
	sort.Strings(set)
	return fmt.Errorf("MCP_AUTH_MODE=%q wertet keine Anbieter-Einstellung aus, gesetzt sind "+
		"aber: %s — diese werden nicht gelesen. Entweder entfernen oder MCP_AUTH_MODE auf "+
		"%q bzw. %q setzen", mode, strings.Join(set, ", "), AuthOIDC, AuthBoth)
}

// resolveEntra baut die Aussteller-URL aus der Verzeichnis-ID.
//
// Warum nur eine GUID zulaessig ist: Der Aussteller im ausgestellten Token
// traegt immer die Verzeichnis-GUID. Eine verifizierte Domain liefert im
// Discovery-Dokument zwar eine Antwort, der darin genannte Aussteller ist aber
// wieder die GUID — die aus der Domain gebaute URL passt also nie zum Token.
// `common`/`organizations` liefern als Aussteller die Vorlage „{tenantid}", die
// sich gegen kein Token pruefen laesst. Beides scheitert sonst erst zur Laufzeit
// als 401-Schleife, die im Client nur als „Authorization failed" ankommt (am
// 09.08.2026 gegen die echten Discovery-Dokumente nachgemessen).
func resolveEntra(cfg *Config, env Env) error {
	tenant := strings.TrimSpace(env("MCP_ENTRA_TENANT_ID"))
	clientID := strings.TrimSpace(env("MCP_ENTRA_CLIENT_ID"))

	if tenant == "" {
		return fmt.Errorf("MCP_ENTRA_TENANT_ID ist bei MCP_OIDC_PROVIDER=%q Pflicht — die "+
			"Verzeichnis-ID (Mandant) aus der Uebersicht der App-Registrierung", ProviderEntra)
	}
	if !guidPattern.MatchString(tenant) {
		return fmt.Errorf("MCP_ENTRA_TENANT_ID = %q ist keine Verzeichnis-ID — erwartet wird "+
			"die GUID aus der Entra-Portal-Uebersicht (Form "+
			"xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx). Eine Domain oder "+
			"`common`/`organizations` funktioniert hier NICHT: der Aussteller im Token "+
			"traegt immer die GUID, `common` liefert nur die Vorlage `{tenantid}`. Die "+
			"Verzeichnis-ID steht im Entra-Portal unter Uebersicht der App-Registrierung", tenant)
	}
	if clientID == "" {
		return fmt.Errorf("MCP_ENTRA_CLIENT_ID ist bei MCP_OIDC_PROVIDER=%q Pflicht — die "+
			"Anwendungs-ID (Client) aus derselben Uebersicht", ProviderEntra)
	}

	cfg.OIDCIssuer = "https://login.microsoftonline.com/" + tenant + "/v2.0"
	cfg.OIDCClientID = clientID
	return nil
}

// resolveAuthentik baut die Aussteller-URL aus Host und Anwendungs-Kuerzel.
// Das Format `https://<host>/application/o/<slug>/` inklusive abschliessendem
// Schraegstrich ist Authentiks Vorgabe (siehe docs/idp/authentik.md).
func resolveAuthentik(cfg *Config, env Env) error {
	baseURL := strings.TrimSpace(env("MCP_AUTHENTIK_BASE_URL"))
	slug := strings.TrimSpace(env("MCP_AUTHENTIK_APP_SLUG"))
	clientID := strings.TrimSpace(env("MCP_AUTHENTIK_CLIENT_ID"))

	if baseURL == "" {
		return fmt.Errorf("MCP_AUTHENTIK_BASE_URL ist bei MCP_OIDC_PROVIDER=%q Pflicht — die "+
			"Adresse der Authentik-Instanz, z. B. https://auth.example.com", ProviderAuthentik)
	}
	// Ein Pfad ist ausdruecklich erlaubt: Authentik laesst sich unter einem
	// Unterpfad betreiben (https://host/authentik), und der muss in der
	// Aussteller-URL erhalten bleiben.
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("MCP_AUTHENTIK_BASE_URL = %q ist keine https-Adresse — erwartet wird "+
			"mindestens Schema und Host, z. B. https://auth.example.com. Laeuft Authentik unter "+
			"einem Unterpfad, gehoert dieser dazu: https://auth.example.com/authentik", baseURL)
	}
	if slug == "" {
		return fmt.Errorf("MCP_AUTHENTIK_APP_SLUG ist bei MCP_OIDC_PROVIDER=%q Pflicht — das "+
			"Kuerzel der Anwendung, wie es in ihrer Authentik-URL steht", ProviderAuthentik)
	}
	if strings.ContainsAny(slug, "/?#") {
		return fmt.Errorf("MCP_AUTHENTIK_APP_SLUG = %q darf keine Pfad- oder Query-Zeichen "+
			"enthalten — nur das Kuerzel selbst", slug)
	}
	if clientID == "" {
		return fmt.Errorf("MCP_AUTHENTIK_CLIENT_ID ist bei MCP_OIDC_PROVIDER=%q Pflicht — die "+
			"Client-ID des OAuth2/OIDC-Providers", ProviderAuthentik)
	}

	cfg.OIDCIssuer = strings.TrimSuffix(baseURL, "/") + "/application/o/" + slug + "/"
	cfg.OIDCClientID = clientID
	return nil
}

// resolveGeneric bedient jeden standardkonformen OpenID-Connect-Anbieter, fuer
// den es hier keinen eigenen Zweig gibt — etwa GitLab oder Keycloak. Er ist ein
// gleichrangiger Weg, kein Ausweichventil fuer Sonderfaelle der beiden anderen:
// Wer Entra nutzt, waehlt entra und bekommt dessen Pruefungen; wer Authentik
// nutzt, waehlt authentik.
func resolveGeneric(cfg *Config, env Env) error {
	issuer := strings.TrimSpace(env("MCP_OIDC_ISSUER"))
	clientID := strings.TrimSpace(env("MCP_OIDC_CLIENT_ID"))

	if issuer == "" {
		return fmt.Errorf("MCP_OIDC_ISSUER ist bei MCP_OIDC_PROVIDER=%q Pflicht — der "+
			"`issuer`-Wert aus dem Discovery-Dokument des Anbieters", ProviderGeneric)
	}
	if clientID == "" {
		return fmt.Errorf("MCP_OIDC_CLIENT_ID ist bei MCP_OIDC_PROVIDER=%q Pflicht", ProviderGeneric)
	}

	cfg.OIDCIssuer = issuer
	cfg.OIDCClientID = clientID
	return nil
}

func ladeAuth(cfg *Config, env Env) error {
	brauchtOIDC := cfg.AuthMode == AuthOIDC || cfg.AuthMode == AuthBoth
	brauchtToken := cfg.AuthMode == AuthToken || cfg.AuthMode == AuthBoth

	if brauchtOIDC {
		if err := resolveProvider(cfg, env); err != nil {
			return err
		}
		// Aussteller und Client-ID sind an dieser Stelle garantiert gesetzt:
		// jeder Anbieter-Zweig in resolveProvider bricht ohne sie ab. Die
		// Client-ID ist zugleich die erwartete Audience — ohne sie wuerde jedes
		// fuer den Aussteller gueltige Token akzeptiert, egal fuer welche
		// Anwendung es ausgestellt wurde. Bei Entra waere das jede beliebige
		// Anwendung desselben Mandanten.
		if cfg.ResourceURL == "" {
			return fmt.Errorf("MCP_RESOURCE_URL ist im Modus %q Pflicht — der Wert muss exakt der "+
				"URL entsprechen, die im Client eingetragen wird", cfg.AuthMode)
		}
		// Gangway (siehe internal/server, ADR-0015) mountet den MCP-Endpunkt
		// fest unter /mcp. PublicBaseURL wird aus ResourceURL abgeleitet,
		// indem genau dieses Suffix wieder abgeschnitten wird — passt
		// ResourceURL nicht dazu, driften die RFC-9728-Metadaten
		// (Resource-URI, WWW-Authenticate-Pointer) von der tatsaechlichen
		// Route auseinander, ohne dass ein Client das laut meldet.
		if !strings.HasSuffix(cfg.ResourceURL, "/mcp") {
			return fmt.Errorf("MCP_RESOURCE_URL = %q muss auf /mcp enden — Gangway mountet den "+
				"MCP-Endpunkt fest unter diesem Pfad (ADR-0015)", cfg.ResourceURL)
		}
		// Im single-Modus ist die Allowlist die einzige Autorisierungsstufe.
		// Ohne sie duerfte jeder Account des IdP auf die Dokumente zugreifen.
		if cfg.AccountMode == ModeSingle && len(cfg.AllowedSubjects) == 0 {
			return fmt.Errorf("MCP_ALLOWED_SUBJECTS ist im Modus %q zusammen mit FILEEE_MODE=single "+
				"Pflicht — leer hiesse: jeder authentifizierte Benutzer des IdP darf zugreifen", cfg.AuthMode)
		}
		// Gangways Server.New verweigert den Start ganz ohne Adress-Freigabeliste
		// (buildAllowList: "no allowlist configured") — ein Server, der niemanden
		// filtern kann, darf nicht hochkommen.
		if len(cfg.AllowedOriginPrefixes) == 0 {
			return fmt.Errorf("FILEEE_ALLOWED_ORIGIN_PREFIXES ist im Modus %q Pflicht — ohne "+
				"Adress-Freigabeliste verweigert Gangway den Start (ADR-0015)", cfg.AuthMode)
		}
	}
	if !brauchtOIDC {
		// Im reinen token-Modus wird keine Anbieter-Einstellung gelesen. Sie
		// still zu ignorieren waere derselbe Fehler, den
		// rejectForeignProviderVariables verhindert: Der Betreiber sucht an
		// einer Stelle, die gar nicht ausgewertet wird.
		if err := rejectOIDCVariables(cfg.AuthMode, env); err != nil {
			return err
		}
		// Ohne Anbieter-Zweig kommt hier niemand vorbei, der den Vorgabewert
		// setzt — ein leeres Feld waere eine stille Falle fuer spaetere Leser.
		if cfg.OIDCSubjectClaim == "" {
			cfg.OIDCSubjectClaim = defaultSubjectClaim(cfg.OIDCProvider)
		}
	}
	if brauchtToken && cfg.APIToken == "" {
		return fmt.Errorf("MCP_API_TOKEN ist im Modus %q Pflicht", cfg.AuthMode)
	}

	if brauchtToken && cfg.ResourceURL != "" && !istLoopback(cfg.ResourceURL) {
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
			"MCP_AUTH_MODE=%q auf der oeffentlich erreichbaren URL %s — der Zugriff auf die Dokumente "+
				"haengt damit an einem einzigen statischen String. Fuer Produktion ist oidc vorgesehen.",
			cfg.AuthMode, cfg.ResourceURL))
	}
	return nil
}

func ladeKonten(cfg *Config, env Env) error {
	cfg.subjectIndex = map[string]string{}

	if cfg.AccountMode == ModeSingle {
		user, pass := env("FILEEE_USERNAME"), env("FILEEE_PASSWORD")
		if user == "" || pass == "" {
			return fmt.Errorf("FILEEE_USERNAME und FILEEE_PASSWORD sind im Modus single Pflicht")
		}
		cfg.Accounts = []Account{{
			Key:      defaultAccountKey,
			Username: user,
			Password: pass,
			TOTPSeed: env("FILEEE_TOTP_SEED"),
			Subjects: cfg.AllowedSubjects,
		}}
		for _, s := range cfg.AllowedSubjects {
			cfg.subjectIndex[s] = defaultAccountKey
		}
		return nil
	}

	// Ein statisches Token traegt kein Subject — im multi-Modus gaebe es nichts
	// aufzuloesen. Bei both bleibt der JWT-Pfad nutzbar, der Token-Pfad nicht.
	if cfg.AuthMode == AuthToken {
		return fmt.Errorf("FILEEE_MODE=multi zusammen mit MCP_AUTH_MODE=token ist nicht aufloesbar: " +
			"ein statisches Token traegt kein Subject, das auf ein Konto zeigen koennte")
	}

	keys := splitListe(env("FILEEE_ACCOUNTS"))
	if len(keys) == 0 {
		return fmt.Errorf("FILEEE_ACCOUNTS ist im Modus multi Pflicht")
	}

	// Zwei Pruefungen auf Eindeutigkeit: der Key selbst wird zum Dateinamen der Session,
	// und das daraus abgeleitete Env-Praefix bestimmt, welche Variablen gelesen werden.
	// "foo-bar" und "foo_bar" ergeben dasselbe Praefix und wuerden sich sonst
	// unbemerkt dieselben Zugangsdaten teilen.
	gesehen := map[string]bool{}
	praefixe := map[string]string{}

	for _, key := range keys {
		if gesehen[key] {
			return fmt.Errorf("der Konto-Key %q steht mehrfach in FILEEE_ACCOUNTS", key)
		}
		gesehen[key] = true

		if !accountKeyMuster.MatchString(key) {
			return fmt.Errorf("der Konto-Key %q ist unzulaessig — erlaubt sind 1 bis 32 Zeichen aus "+
				"[a-z0-9_-]; der Key wird als Dateiname der Session verwendet", key)
		}
		praefix := "FILEEE_ACCOUNT_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		if anderer, kollision := praefixe[praefix]; kollision {
			return fmt.Errorf("die Konto-Keys %q und %q lesen dieselben Variablen (%s_*) — "+
				"Bindestrich und Unterstrich werden im Praefix gleich behandelt", anderer, key, praefix)
		}
		praefixe[praefix] = key

		konto := Account{
			Key:      key,
			Username: env(praefix + "_USERNAME"),
			Password: env(praefix + "_PASSWORD"),
			TOTPSeed: env(praefix + "_TOTP_SEED"),
			Subjects: splitListe(env(praefix + "_SUBJECTS")),
		}
		if konto.Username == "" || konto.Password == "" {
			return fmt.Errorf("%s_USERNAME und %s_PASSWORD sind Pflicht", praefix, praefix)
		}

		for _, subject := range konto.Subjects {
			if vorhanden, doppelt := cfg.subjectIndex[subject]; doppelt {
				return fmt.Errorf("das Subject %q zeigt auf zwei Konten (%q und %q) — bei zwei plausiblen "+
					"Zuordnungen gibt es keine richtige Wahl, deshalb kein first-match-wins",
					subject, vorhanden, key)
			}
			cfg.subjectIndex[subject] = key
		}
		cfg.Accounts = append(cfg.Accounts, konto)
	}
	return nil
}

func orDefault(wert, fallback string) string {
	if strings.TrimSpace(wert) == "" {
		return fallback
	}
	return strings.TrimSpace(wert)
}

func splitListe(roh string) []string {
	var out []string
	for _, teil := range strings.Split(roh, ",") {
		if t := strings.TrimSpace(teil); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// maxRequestBodyBytesFor berechnet MaxUploadBytes*4/3 + 64 KiB — dieselbe
// Formel, die LoadConfig frueher direkt inline schrieb (Base64s
// Aufblaeh-Faktor 4/3 plus ein 64-KiB-Rahmen fuer JSON-RPC/HTTP) —
// saettigt aber auf math.MaxInt64, wenn und NUR wenn das tatsaechliche
// Endergebnis (inklusive des Rahmen-Zuschlags) nicht mehr in int64
// passt.
//
// FILEEE_MAX_UPLOAD_BYTES akzeptiert jeden nicht-negativen int64-Wert
// (intWert unten weist nur NEGATIVE Werte zurueck) — ein Betreiber kann
// ihn also beliebig nah an math.MaxInt64 setzen. Eine erste Fassung
// dieser Funktion pruefte den EINGABEWERT direkt gegen math.MaxInt64/4,
// bevor ueberhaupt gerechnet wurde (dieselbe Form wie die urspruengliche
// Inline-Formel) — das saettigte aber deutlich zu FRUEH: Fuer
// Upload-Limits zwischen math.MaxInt64/4 und rund 3*math.MaxInt64/4
// ueberlief zwar die NAIVE Zwischenrechnung maxUploadBytes*4, das
// tatsaechlich gewuenschte Endergebnis maxUploadBytes*4/3 + 64<<10
// passte aber noch bequem in int64 — die Pruefung schuetzte also vor
// dem Ueberlauf einer Zwischengroesse, die diese Funktion gar nicht
// mehr braucht, sobald man die Rechnung anders aufteilt, und lieferte
// dafuer einen viel LOCKEREREN Wert als konfiguriert, statt eines
// genauen (Fund: Codex-Review, 23.08.2026, Beispiel maxUploadBytes =
// 3000000000000000000 — muesste 4000000000000065536 ergeben, lieferte
// aber math.MaxInt64).
//
// Die Loesung ist die uebliche Umformung ueber Quotient und Rest:
// n*4/3 == (n/3)*4 + (n%3)*4/3 — jeder Teilterm bleibt dabei klein
// genug, um NICHT vorzeitig zu ueberlaufen, waehrend das mathematische
// Ergebnis unveraendert bleibt (n = 3*(n/3) + (n%3), also n*4 =
// 3*(n/3)*4 + (n%3)*4, und der erste Summand ist durch 3 teilbar —
// die Division /3 verteilt sich exakt auf beide Anteile). (n%3)*4/3
// ist immer klein (n%3 in {0,1,2}, also max. 8/3), (n/3)*4 kann NUR
// dann noch ueberlaufen, wenn n/3 bereits groesser als math.MaxInt64/4
// ist — und GENAU dann ist auch das wahre, unbeschraenkte Endergebnis
// bereits groesser als math.MaxInt64 (denn (math.MaxInt64/4 + 1)*4
// liegt schon ueber math.MaxInt64), saettigen ist an dieser Stelle also
// nicht verfrueht, sondern der fruehestmoegliche Punkt, an dem das
// Endergebnis nachweislich nicht mehr passt. Das ist dieselbe
// Ueberlauf-Klasse, vor der internal/tools/write_documents.go's
// base64EncodedLenFor beim Groessen-Gate fuer die kodierte Laenge
// schuetzt — dort aber bereits von Anfang an ohne dieses Problem, weil
// die Division dort VOR der Multiplikation steht (ceil(n/3)*4, eine
// einzelne, bereits praezise Multiplikation) statt wie hier zuerst zu
// multiplizieren und danach zu dividieren.
//
// Hier steht bewusst eine zweite, eigenstaendig kommentierte
// Implementierung statt eines gemeinsamen Helfers mit
// base64EncodedLenFor: beide liegen in verschiedenen Paketen (hier
// internal/config, dort internal/tools) und berechnen zwei
// UNTERSCHIEDLICHE Ausdruecke (dieser hier addiert obendrauf einen
// festen Rahmen-Zuschlag und rundet ab, statt auf den naechsten
// Base64-Block aufzurunden) — ein gemeinsames Paket ueber diese
// Paketgrenze hinweg fuer wenige Zeilen saettigender Arithmetik waere
// mehr Aufwand als die Duplikation, die es einsparen wuerde.
//
// Saettigung (statt auf 0 abzuschneiden oder in Panic zu geraten) haelt
// MaxRequestBodyBytes einen gueltigen, nutzbaren — wenn auch absurd
// grosszuegigen — Byte-Deckel, statt einen Wert, der den Server jeden
// Upload ablehnen liesse (siehe base64EncodedLenFor's eigenen
// Doc-Kommentar dafuer, warum ein gesaettigter, aber positiver Deckel
// im Sinne dieses Werts weiterhin ein korrekter Deckel ist).
func maxRequestBodyBytesFor(maxUploadBytes int64) int64 {
	const rahmenZuschlag = 64 << 10 // 64 KiB Spielraum fuer JSON-RPC/HTTP-Rahmen

	quotient, rest := maxUploadBytes/3, maxUploadBytes%3
	// quotient*4 kann selbst noch ueberlaufen, wenn maxUploadBytes so
	// gross ist, dass schon dieser Anteil nicht mehr in int64 passt —
	// und genau dann ist auch das wahre Endergebnis (das noch groesser
	// waere) garantiert nicht mehr darstellbar. Hier zu saettigen
	// verliert also keine Praezision gegenueber einer spaeteren Pruefung.
	if quotient > math.MaxInt64/4 {
		return math.MaxInt64
	}
	// rest ist immer 0, 1 oder 2 -- rest*4 (max. 8) kann nie ueberlaufen.
	aufgeblaeht := quotient*4 + (rest*4)/3

	if aufgeblaeht > math.MaxInt64-rahmenZuschlag {
		return math.MaxInt64
	}
	return aufgeblaeht + rahmenZuschlag
}

func intWert(env Env, key string, fallback int64) (int64, error) {
	roh := strings.TrimSpace(env(key))
	if roh == "" {
		return fallback, nil
	}
	wert, err := strconv.ParseInt(roh, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s = %q ist keine ganze Zahl", key, roh)
	}
	// Negative Werte sind fuer jeden Konsumenten dieser Funktion unsinnig — Byte-Grenzen,
	// Burst-Groessen, Nebenlaeufigkeit. Ohne diese Pruefung ergaebe ein negatives
	// Upload-Limit ein negatives MaxRequestBodyBytes, und der Server startete damit.
	if wert < 0 {
		return 0, fmt.Errorf("%s = %q darf nicht negativ sein", key, roh)
	}
	return wert, nil
}

func floatWert(env Env, key string, fallback float64) (float64, error) {
	roh := strings.TrimSpace(env(key))
	if roh == "" {
		return fallback, nil
	}
	wert, err := strconv.ParseFloat(roh, 64)
	if err != nil {
		return 0, fmt.Errorf("%s = %q ist keine Zahl", key, roh)
	}
	if wert < 0 {
		return 0, fmt.Errorf("%s = %q darf nicht negativ sein", key, roh)
	}
	return wert, nil
}

func dauerWert(env Env, key string, fallback time.Duration) (time.Duration, error) {
	roh := strings.TrimSpace(env(key))
	if roh == "" {
		return fallback, nil
	}
	wert, err := time.ParseDuration(roh)
	if err != nil {
		return 0, fmt.Errorf("%s = %q ist keine Dauer (erwartet z. B. 15m, 30s)", key, roh)
	}
	if wert < 0 {
		return 0, fmt.Errorf("%s = %q darf nicht negativ sein", key, roh)
	}
	return wert, nil
}

// istLoopback erkennt lokale Adressen, bei denen der token-Modus unbedenklich ist.
//
// Die Auswertung laeuft ueber url.Parse und Hostname(), nicht ueber eigenes
// Zerschneiden: nur so wird die Klammer-Schreibweise von IPv6 ("http://[::1]:8080/")
// korrekt aufgeloest. Eine selbstgebaute Trennung am ersten Doppelpunkt haette
// dort "[" ergeben und faelschlich vor einer oeffentlichen URL gewarnt.
func istLoopback(roh string) bool {
	u, err := url.Parse(roh)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

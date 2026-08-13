// Package destinations defines the pluggable delivery seam ADR 0016
// creates: a Destination interface plus a name -> constructor registry.
//
// A destination module (Paperless-ngx, NFS, SMB, a generic HTTP-POST
// target, fileee, ...) is a self-contained Go package that implements
// Destination and calls Register from its own init(), matching ADR
// 0016's "adding a destination = new package + registration call, the
// dispatch core is not modified" model. scan-bridge's main.go
// blank-imports only the destination packages it wants compiled in
// (see docs/superpowers/specs/2026-08-13-scan-paperless-pipeline-design.md
// sec. 5.1); v1 blank-imports only the paperless package.
//
// This package itself contains no destination module — it is the seam,
// not a tenant of it. The only fully-implemented module in v1 is
// internal/destinations/paperless (a later task); nfs, smb, httppost,
// and fileee are registry slots reserved by the design but not built
// here.
package destinations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
)

// Document is what scan-processor produced for one destination-bound
// object: one per profile.assembly.page_grouping="combined" job, or
// one per page when "per_page" (ADR 0017).
type Document struct {
	// ID is scan-bridge's scan_id, for correlation across destinations
	// when a profile fans out to more than one target.
	ID string
	// Filename is the suggested on-disk/upload name, e.g.
	// "2026-08-13T14-32-01_receipt.pdf".
	Filename string
	// Content is the assembled document bytes, format per the
	// profile's Format field. Callers own advancing/closing it;
	// Destination.Deliver reads it exactly once.
	Content io.Reader
	// ContentType is the MIME type matching Format.
	ContentType string
	// PageCount is the number of source pages assembled into this
	// document.
	PageCount int
	// DocType is the profile's document-type key (ADR 0017), e.g.
	// "eingangsrechnung". Empty when the profile has no document_type
	// set.
	DocType string
}

// Metadata is the destination-agnostic hint set a destination module
// interprets for the fields it understands; it does not need to
// understand fields it doesn't (ADR 0017).
type Metadata struct {
	Title         string
	Created       *time.Time
	TagIDs        []int    // Paperless-style; unused by destinations without a tag concept.
	Labels        []string // Generic label set (fileee, httppost, ...).
	Correspondent *int
	DocumentType  *int
	ASN           *int
	Extra         map[string]string // Destination-specific passthrough.
}

// ProfileDestinationConfig is one entry of a profile's destinations
// list (ADR 0016 sec. "Profile schema extensions"). Target selects
// which registered Destination module handles delivery; StorageFirst
// chooses NFS/SMB-style intermediate storage vs. a direct API call;
// Config carries the destination-specific block whose shape only that
// module's Constructor understands (e.g. paperless.Config's base_url/
// token_secret/tag_ids/document_type_map) — this package does not
// interpret it.
type ProfileDestinationConfig struct {
	Target       string
	StorageFirst bool
	Config       map[string]any
}

// Destination is implemented by each built-in destination module (ADR
// 0016). Deliver sends one assembled Document, labelled by meta, to
// the destination described by cfg. Deliver must not retain doc.Content
// beyond the call.
type Destination interface {
	// Name returns the destination's registered name, matching the
	// name it was constructed under.
	Name() string
	// Deliver sends doc to this destination, using meta for the
	// fields this destination understands and cfg for its own
	// per-profile configuration. A nil error means the destination
	// accepted the document; for destinations with asynchronous
	// server-side processing (e.g. Paperless-ngx's Celery consumption
	// task), "accepted" means "submitted", not "fully processed" —
	// each module documents what its own nil error means.
	Deliver(ctx context.Context, doc Document, meta Metadata, cfg ProfileDestinationConfig) error
}

// Constructor builds a Destination from its profile-level config block
// plus the shared secret resolver. Registered destination modules pass
// a Constructor to Register from their own init().
type Constructor func(cfg ProfileDestinationConfig, secrets config.SecretResolver) (Destination, error)

// ErrUnknownDestination is wrapped (via %w) in the error Build returns
// when name has no registered Constructor — either a typo in a
// profile's destinations[].target, or a destination module that is not
// yet built/blank-imported.
var ErrUnknownDestination = errors.New("destinations: unknown destination")

var (
	mu       sync.RWMutex
	registry = make(map[string]Constructor)
)

// Register adds a destination constructor under name, so a later Build
// call for that name resolves to ctor. Called from each destination
// module's own init() (ADR 0016's "new destination = new package +
// registration call, core untouched").
//
// Register panics on an empty name, a nil ctor, or a duplicate name —
// each is a programming error caught at process-startup time (modules
// only ever call Register from init()), not a runtime condition a
// caller should recover from. This mirrors the standard library's
// database/sql.Register and image.RegisterFormat.
func Register(name string, ctor Constructor) {
	if name == "" {
		panic("destinations: Register called with empty name")
	}
	if ctor == nil {
		panic(fmt.Sprintf("destinations: Register(%q) called with nil constructor", name))
	}

	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("destinations: duplicate Register(%q)", name))
	}
	registry[name] = ctor
}

// Build looks up the constructor registered under name and invokes it
// with cfg and secrets. If name has no registered constructor, Build
// returns an error wrapping ErrUnknownDestination (checkable via
// errors.Is) that also lists the currently known names, to make a
// profile-config typo easy to spot. If the constructor itself fails
// (e.g. cfg does not decode into the destination's own config shape),
// that error is wrapped and returned unchanged otherwise.
func Build(name string, cfg ProfileDestinationConfig, secrets config.SecretResolver) (Destination, error) {
	mu.RLock()
	ctor, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("destinations: %w: %q (known: %v)", ErrUnknownDestination, name, Names())
	}

	dest, err := ctor(cfg, secrets)
	if err != nil {
		return nil, fmt.Errorf("destinations: build %q: %w", name, err)
	}
	return dest, nil
}

// Names returns the currently registered destination names, sorted for
// deterministic output (error messages, future /profiles diagnostics).
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/config"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/destinations"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/procclient"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/profiles"
	"github.com/strausmann/paperless-scan-bridge/components/scan-bridge/internal/tag"
)

// destinationBuild is one profile.DestinationConfigs() entry, resolved
// to a built destinations.Destination (or the error building it
// failed with — an unknown destinations[].target typo, or a
// destination module's own config validation, e.g. the paperless
// module's missing/invalid base_url).
//
// Building is done once per destination per handleScan call, ahead of
// the per-document delivery loop (scan.go's handleScan) — a
// destination's cfg does not vary per document within one scan, so
// there is no reason to re-parse/re-validate it once per assembled
// document.
type destinationBuild struct {
	cfg  destinations.ProfileDestinationConfig
	dest destinations.Destination
	err  error
}

// buildDestinations resolves every one of a profile's configured
// destinations via destinations.Build (ADR 0016). A build failure for
// one destination is recorded on that destinationBuild's err field,
// not returned as an overall error — deliverDocument turns it into a
// per-document, per-destination "failed" result, consistent with the
// design doc's "a destination failure never aborts delivery to other
// destinations" contract (sec. 8) applying just as much to a
// build-time failure as to a Deliver-time one.
func buildDestinations(configs []destinations.ProfileDestinationConfig, secrets config.SecretResolver) []destinationBuild {
	out := make([]destinationBuild, 0, len(configs))
	for _, cfg := range configs {
		dest, err := destinations.Build(cfg.Target, cfg, secrets)
		out = append(out, destinationBuild{cfg: cfg, dest: dest, err: err})
	}
	return out
}

// deliverDocument delivers one assembled document to every one of the
// profile's configured destinations, returning one destinationResult
// per destination in the same order as builds. A failure delivering to
// one destination does not stop delivery to the next (design doc sec.
// 8).
func deliverDocument(
	ctx context.Context,
	logger *slog.Logger,
	scanID string,
	profile profiles.Profile,
	doc procclient.Document,
	builds []destinationBuild,
	callerTagIDs []int,
	callerStrategy tag.Strategy,
) []destinationResult {
	out := make([]destinationResult, 0, len(builds))
	for _, b := range builds {
		if b.err != nil {
			out = append(out, destinationResult{
				Name:   b.cfg.Target,
				Status: "failed",
				Error:  b.err.Error(),
			})
			continue
		}
		out = append(out, deliverToDestination(ctx, logger, scanID, profile, doc, b, callerTagIDs, callerStrategy))
	}
	return out
}

// deliverToDestination opens the document scan-processor wrote to
// doc.Path, builds the destinations.Document/Metadata pair for it, and
// calls b.dest.Deliver. The file is opened fresh for every destination
// (rather than once and shared) because destinations.Destination.Deliver
// "must not retain doc.Content beyond the call" and there is no
// guarantee a second destination could re-read an io.Reader a first
// destination already consumed — this mirrors how a caller with N
// destinations for one document must produce N independent readers
// over the same underlying bytes.
func deliverToDestination(
	ctx context.Context,
	logger *slog.Logger,
	scanID string,
	profile profiles.Profile,
	doc procclient.Document,
	b destinationBuild,
	callerTagIDs []int,
	callerStrategy tag.Strategy,
) destinationResult {
	f, err := os.Open(doc.Path)
	if err != nil {
		return destinationResult{
			Name:   b.cfg.Target,
			Status: "failed",
			Error:  fmt.Sprintf("open assembled document: %v", err),
		}
	}
	defer func() { _ = f.Close() }()

	ddoc := destinations.Document{
		ID:          scanID,
		Filename:    doc.Filename,
		Content:     f,
		ContentType: doc.ContentType,
		PageCount:   doc.PageCount,
		DocType:     profile.DocumentType,
	}
	meta := resolveMetadata(profile, b.cfg, callerTagIDs, callerStrategy)

	if err := b.dest.Deliver(ctx, ddoc, meta, b.cfg); err != nil {
		if logger != nil {
			logger.WarnContext(ctx, "destination delivery failed",
				slog.String("scan_id", scanID),
				slog.String("destination", b.cfg.Target),
				slog.String("filename", doc.Filename),
				slog.Any("err", err))
		}
		return destinationResult{
			Name:   b.cfg.Target,
			Status: "failed",
			Error:  err.Error(),
		}
	}
	return destinationResult{
		Name:   b.cfg.Target,
		Status: "submitted",
	}
}

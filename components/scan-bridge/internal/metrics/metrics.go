// Package metrics owns the Prometheus collectors exported on the
// dedicated metrics listener documented in CONTAINER_SUITE.md sec. 4.8.
//
// Phase 1.1 surfaces only build_info — the real job, dispatch, scan,
// and processing histograms are added once the dispatch and jobs
// subsystems land. The empty Registry is shared between this package
// and main.go so the wiring is the same shape today and tomorrow.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collectors bundles every collector this binary publishes. main.go
// registers it on a private prometheus.Registry; the global default
// registry is intentionally not used so accidental third-party
// imports cannot pollute our exposition.
type Collectors struct {
	BuildInfo *prometheus.GaugeVec

	// TODO(phase 1.4): add scan_bridge_jobs_total, the per-stage
	// duration histograms, queue_depth, active_jobs, and the API
	// request counters/histograms once the jobs and dispatch
	// subsystems have something to record.
}

// New builds an unregistered Collectors set with build_info already
// populated to 1 for the supplied labels.
func New(version, commit, buildDate string) *Collectors {
	c := &Collectors{
		BuildInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "scan_bridge_build_info",
				Help: "Build identity of the running scan-bridge binary.",
			},
			[]string{"version", "commit", "build_date"},
		),
	}
	c.BuildInfo.WithLabelValues(version, commit, buildDate).Set(1)
	return c
}

// Register adds every collector in c to reg. It is a separate step
// from New so tests can construct a Collectors without polluting any
// registry.
func (c *Collectors) Register(reg prometheus.Registerer) error {
	return reg.Register(c.BuildInfo)
}

// Handler returns an http.Handler that serves the registry in
// Prometheus exposition format. Mount this on the metrics listener
// from main.go.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

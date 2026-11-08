package api

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewMetricsHandler serves only the default Prometheus registry. Keep it on a
// separate listener: metrics scrapers must not reach the identity-trusting API.
// No API router, authentication middleware, storage or device hub is installed.
func NewMetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
}

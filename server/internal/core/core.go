// Package core defines how the panel talks to the proxy core (sing-box).
//
// Milestone 1 targets the OFFICIAL sing-box build, which has no v2ray API;
// the only usable surface is the Clash API. This package therefore speaks
// Clash API only. It imports no other internal module: consumers depend on
// the CoreClient interface declared here.
package core

import "context"

// Totals is a single point-in-time reading of core traffic counters.
type Totals struct {
	UploadTotal   int64 // cumulative bytes uploaded since core start
	DownloadTotal int64 // cumulative bytes downloaded since core start
	ActiveConns   int   // currently open connections
}

// CoreClient is the seam between the panel and the proxy core.
type CoreClient interface {
	// Version returns the core version string, e.g. "sing-box 1.14.3".
	Version(ctx context.Context) (string, error)
	// Totals returns cumulative traffic counters and live connection count.
	Totals(ctx context.Context) (Totals, error)
}

// Package device implements the DPS-150 device hub: the single owner of the
// device connection. The hub dials the transport with an exponential-backoff
// reconnect loop, decodes the RX stream into a cached state and fans out
// typed updates (state snapshots, telemetry, status and device events) to
// subscribers. The hub reports connected only once the device has answered
// the handshake with a full state dump, so a connected snapshot always
// carries a state. All writes to the device go through the hub and are
// serialized and time-bounded, so concurrent API calls never interleave on
// the wire and a stalled transport is dropped instead of wedging commands.
package device

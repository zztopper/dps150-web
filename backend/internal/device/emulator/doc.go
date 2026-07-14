// Package emulator implements an in-memory FNIRSI DPS-150 device emulator
// behind a transport.Dialer, byte- and frame-compatible with the real
// hardware (docs/FNIRSI_DPS-150_Protocol.md): the driver must not be able to
// tell it apart from a physical supply. It backs the mock:// transport for
// e2e tests in CI and for development without hardware.
//
// Behaviour mirrors the protocol reference:
//
//   - Until the host sends the session-enable frame the device is silent:
//     no telemetry, no responses; non-session frames are ignored entirely
//     (no state change). Session state is per connection and resets on
//     reconnect.
//   - After session enable the device pushes telemetry every 500 ms
//     (WithTelemetryInterval shortens it for tests): input voltage C0,
//     measurement C3 (V/I/P), temperature C4, max voltage E2, max
//     current E3, plus capacity D9 and energy DA while metering is enabled
//     and the output is on. The first burst is pushed immediately on
//     session enable.
//   - Register writes are confirmed with an RX echo (same register and
//     payload, group A1), exactly as the protocol examples show. Writes to
//     read-only or unknown registers are ignored without a reply.
//   - Reads of single registers and of the full dump (register FF, both the
//     LEN=0 GetAll form and the LEN=1 refresh form from the protocol doc)
//     answer with the current state; the FF dump is FullDumpSize bytes and
//     decodes with protocol.Decode.
//   - On-change frames DB (output), DC (protection) and DD (CC/CV) are
//     pushed unsolicited when the corresponding state changes.
//
// The output is loaded with an ideal resistor R (default 10 Ω, replaceable
// via (*Device).SetLoadResistance): the supply regulates in CV when
// Vset/R <= Iset (V = Vset, I = V/R) and in CC otherwise (I = Iset,
// V = I*R); P = V*I. With the output off all measurements are zero.
//
// Protection thresholds D1–D5 are stored and enforced against the measured
// values (OVP/OCP/OPP against V/I/P, OTP against the internal temperature,
// LVP against the input voltage). A trip pushes a DC frame with the
// protection code, switches the output off and pushes a DB frame.
//
// Decisions the protocol reference leaves open, fixed here:
//
//   - A tripped protection latches: it is cleared only by a host write to
//     RegOutputEnable (either value), which pushes a DC frame with
//     ProtectionOK. If the offending condition persists, the protection
//     trips again on the next evaluation.
//   - ProtectionREP (reverse polarity) can never arise from the load model;
//     it is only reachable through the (*Device).TripProtection test hook.
//   - Enabling metering (RegMeteringEnable 0 -> 1) restarts the Ah/Wh
//     accumulators from zero; disabling stops accumulation and keeps the
//     values readable.
//   - The device port is single-client, like the real USB CDC port: a new
//     Dial supersedes a still-open connection, whose reads then report EOF.
//
// The package depends on the standard library, internal/device/protocol and
// internal/transport only, and performs no real I/O.
package emulator

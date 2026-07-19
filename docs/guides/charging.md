# Battery charging & health

The **Charge** page turns the DPS-150 into a supervised CC-CV battery charger and
tracks the health of your cells over their lifetime. It has four tabs — **Charge**
(start / live), **Profiles**, **History** and **Batteries**.

> ⚠️ Charging batteries is inherently risky. The app enforces a strict,
> non-disable-able safety envelope (below), but you are responsible for using the
> right chemistry, cell count and current for your pack, and for never leaving a
> charge unattended.

![Battery charging](../screenshots/charge.png)

## Charging a battery (F-023)

The charger is a backend-supervised state machine — the charge runs on the server
and keeps going regardless of your browser. A charge is a chemistry preset
compiled into an ordered list of phases:

`preflight → [precharge] → CC → CV → [absorb / float for Pb] → done`

Termination is decided from the **measured** voltage and current (current tapered
below the cutoff while the pack is at the charge voltage), never from the device's
advisory CC/CV flag alone.

### Supported chemistries

| Chemistry | Per-cell charge / cutoff | Notes |
|---|---|---|
| **Li-ion** | 4.20 V CV, taper C/20 | precharge below 3.0 V/cell |
| **LiFePO4** | 3.65 V CV | precharge below 2.5 V/cell, no float |
| **Lead-acid (Pb)** | 2.40 V absorb → 2.25 V float | float held until you stop |

Multi-cell lithium packs (`cells ≥ 2`) require an **attested BMS/balancer** — the
DPS-150 charges the whole pack with no per-cell control, so an imbalance can push
one cell over-voltage at a safe pack average. NiMH is intentionally not offered
(no autonomous −ΔV termination and no *battery* temperature sensor on this rig).

### Steps

1. **Create a charge profile** (Profiles tab): name, chemistry, cell count, pack
   capacity (mAh) and charge current. Capacity feeds the safety cap and the
   `equivalent cycles` metric.
2. Open the **Charge** tab and pick the profile.
3. **Pre-flight** runs first: it turns the output *off*, waits for the terminal
   voltage to settle and reads the pack's open-circuit voltage. It **refuses to
   start** on a wrong cell count (adjacent counts alias — a full 2S ≈ a flat 3S),
   a reverse-polarity / negative reading, or no battery (≈ 0 V). The dialog shows
   the computed limits; starting is an explicit confirmed action (enabling the
   output is always confirmed).
4. Watch the **live** view: the V + I chart with phase bands, the current phase,
   elapsed / ETA, delivered mAh·/Wh and the safety-cap progress bars.

### Safety envelope (always on)

- **Start order** protections → V-setpoint → I-setpoint → output-on, so the output
  never energises with a stale setpoint.
- **Telemetry-staleness watchdog** — if telemetry stops for a few seconds the
  charge faults and the output is cut (a hung device over a raw-TCP bridge does
  *not* surface as a link loss, so this, not link state, is the primary trip).
- **`SafeOutputOff` on every exit** (finish, stop, protection trip, fault) — a
  fresh, retried, telemetry-confirmed output-off; an unconfirmed off escalates to
  an alarm.
- Per-phase timeouts, an absolute voltage ceiling (software + hardware OVP), a
  capacity cap (115–125 % of rated), and the device OVP/OCP/OPP/OTP from the
  profile.
- A **shared interlock** — while a charge owns the output, manual control, IV
  sweeps and sequences are blocked with `409` (and vice-versa).
- **Startup reconciliation** — if the backend crashed mid-charge, on restart it
  finalises the orphaned session and cuts any stray energised output.

## Battery health & cycle tracking (F-026)

The **Batteries** tab is a library of your physical packs. Each finished charge
can be assigned to a battery, and the app derives that battery's health from the
accumulated charges.

![Battery health](../screenshots/battery-health.png)

### The honest-capacity gate — read this

`deliveredMah` is the **charge accepted in one session**, *not* the battery's
capacity. A `completed` charge only means the CC-CV taper fired (the pack ended
full) — it says nothing about where it *started*. A routine top-up from 80 % is a
`completed` charge that delivers very little, so treating every completed charge
as a capacity reading would report a healthy battery as failing.

Because the DPS-150 is a source and cannot discharge, there is no capacity-test
mode. Instead the charger records the **start open-circuit voltage**, and a
session only counts as a **capacity data-point** when it was a genuine *from-empty*
charge — its per-cell start voltage was at or below the chemistry's "empty"
threshold (Li-ion ≤ 3.00 V, LiFePO4 ≤ 2.50 V, Pb ≤ 1.90 V/cell). Everything else
(top-ups, aborted charges, and pre-F-026 sessions with no recorded start voltage)
is still listed against the battery but flagged **"not a capacity measurement"**
and excluded from the capacity numbers.

### Using it

1. **Create a battery** (Batteries tab → New): name, chemistry, cell count, an
   optional rated capacity (mAh) and part number / notes. Chemistry and cell count
   are fixed once created.
2. From the **History** tab, **assign** a finished charge session to a battery
   ("Assign to battery"). The session's chemistry and cell count must match the
   battery. You can reassign or unassign at any time.
3. Open the battery to see its health.

### The health metrics

- **SoH (state of health)** — `latest / rated` capacity if you set a rated
  capacity, otherwise `latest / best`. Shown with a bar (clamped to 100 %) and the
  true number (which can exceed 100 % for a strong pack).
- **Degradation** — `1 − latest / best`, the drop from the best from-empty charge
  ever seen (0 % at peak).
- **Full cycles** — the count of genuine from-empty (capacity-eligible) charges.
- **Equivalent cycles** — `Σ delivered mAh / rated`, a throughput-based wear proxy
  over *all* completed charges (needs a rated capacity).
- **Latest / best / first capacity** and **total energy** (Wh).
- **Capacity-degradation curve** — capacity (mAh) over time, plotted from exactly
  the same capacity-eligible set the SoH number uses, so the chart and the headline
  never disagree.

> **Lead-acid caveat:** Pb charging is coulombically inefficient (~80–90 %) — the
> delivered mAh includes charge lost to gassing/heat, so Pb "capacity" is
> overstated. Read the Pb trend as relative, not absolute.

Internal resistance (Rint) tracking is planned for a future version — it needs an
additional in-charge measurement.

## See also

- [IV curve tracer & component library](iv-tracer.md)
- Design: `docs/architecture/design.md` §3.7 (charging), §3.10 (battery health)

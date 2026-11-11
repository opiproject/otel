# OPI storage snapshot to OTLP conversion

This reference conversion component accepts existing OPI storage `Stats*`
responses and their optional sampling context. It produces standard OTLP
`ResourceMetrics`; it is not another polling agent, transport, or exporter.
Feed the result into the existing OTLP SDK/export path and Collector pipeline.

## Semantics

- Bytes and operation totals are absolute, monotonic cumulative sums.
  Samples 100 then 120 remain 100 then 120, not `Counter.Add(100)` followed by
  `Counter.Add(120)`.
- The first observation without a known counter start is an OTLP zero-duration
  unknown-start reset point. Subsequent points retain that observation boundary.
- A known epoch/start change or an observed decrease establishes a new baseline
  and suppresses interval metrics for that sample.
- Tick-frequency changes or source-clock regressions suppress the affected
  interval without claiming that the I/O counters themselves reset.
- Known source ticks determine throughput/IOPS intervals; wall-clock observation
  time is not substituted for an unknown source clock.
- Mean latency is `delta_latency_ticks / tick_rate_hz / delta_operations`.
  No operations means no latency measurement, not an artificial zero. No
  histogram, p99, or cross-host clock correlation is invented.

The converter cannot detect a reset that happens between polls and grows past
the previous value when the provider supplies no epoch/start marker. SPDK
iostat currently has this limitation. Unsupported/negative sentinel counters,
invalid clocks, and out-of-order observation timestamps fail explicitly.
Unknown frequency is a reported `IntervalStatus`, not an assumed conversion.
Intervals with a known clock but no source epoch/start marker are explicitly
reported as `inferred_continuity`, not proven uninterrupted counter history.

## Integration

Pass a stable, caller-controlled DPU source identity and the OPI resource name,
plus `response.GetStats()` and `response.GetSampleMetadata()`, to `Convert`.
Keep the returned `Next` state separately for each source/resource pair; discard
it on resource deletion and bound any caller-owned cache.

The state owns cloned inputs. A failed conversion does not advance it. The
converter does not use a global registry, add arbitrary tenant labels, or create
a new timeseries for each reset token. Applications remain responsible for tenant
authorization, source identity, and export/sampling budgets.

`example_test.go` shows a complete conversion from an actual OPI response type.
`Result.Metrics` fits directly in an OTLP `ExportMetricsServiceRequest`.

The API and bridge sampling-context changes are prerequisites for providers to
populate this input. Legacy responses without metadata are rejected rather than
given fabricated timestamps or units. Existing counter-width limitations remain
separate; this adapter rejects negative/unsupported values instead of repairing
overflowed inputs.
The explicit API fork replacement is pinned to opiproject/opi-api#511 while that
contract is under review. Remove the replacement and use its upstream module
version after merge.

## Validation

```bash
go test ./...
go vet ./...
```

Tests cover absolute totals, standard OTLP binary output, tick counts above
2^53, zero-duration first points, observed/explicit resets, unknown/changing
clocks, idle windows, out-of-order samples, timestamp overflow, scope isolation,
and input/state ownership.

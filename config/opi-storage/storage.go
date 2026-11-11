// SPDX-License-Identifier: Apache-2.0

// Package storageotel converts OPI storage snapshots into OTLP metrics.
package storageotel

import (
	"fmt"
	"math"

	pb "github.com/opiproject/opi-api/storage/v1alpha1/gen/go"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Sample identifies one resource's cumulative statistics and observation context.
type Sample struct {
	SourceID     string
	ResourceName string
	Stats        *pb.VolumeStats
	Metadata     *pb.StatsSampleMetadata
}

// WindowStatus explains whether interval metrics could be derived.
type WindowStatus string

const (
	WindowFirstSample    WindowStatus = "first_sample"
	WindowReady          WindowStatus = "ready"
	WindowInferred       WindowStatus = "inferred_continuity"
	WindowIdle           WindowStatus = "no_completed_operations"
	WindowReset          WindowStatus = "counter_reset"
	WindowClockUnknown   WindowStatus = "clock_unknown"
	WindowClockChanged   WindowStatus = "clock_changed"
	WindowClockUnchanged WindowStatus = "clock_unchanged"
)

// State is caller-owned state for one source/resource pair, not a global cache.
// Callers should discard it when the resource is deleted.
type State struct {
	sample    Sample
	values    counters
	time      uint64
	startTime uint64
}

// Result contains standard OTLP output and explicit interval availability.
// Next can be passed to the next Convert call for this source/resource pair.
type Result struct {
	Metrics        *metrics.ResourceMetrics
	Next           State
	IntervalStatus WindowStatus
}

type counters struct {
	bytes [3]int64
	ops   [3]int64
	ticks [3]int64
}

// Convert emits absolute cumulative totals, never Counter.Add(total) increments.
// Missing clock data suppresses derived metrics without fabricating a frequency.
// Invalid samples return an error and do not advance caller state.
func Convert(current Sample, previous *State) (Result, error) {
	now, values, err := validate(current)
	if err != nil {
		return Result{}, err
	}
	start := now
	if current.Metadata.CounterStartTime != nil {
		start, err = timestampNanos(current.Metadata.CounterStartTime)
		if err != nil {
			return Result{}, fmt.Errorf("counter start time: %w", err)
		}
	}
	reportedStart := start
	window := WindowFirstSample
	if previous != nil {
		if previous.sample.SourceID != current.SourceID || previous.sample.ResourceName != current.ResourceName {
			return Result{}, fmt.Errorf("previous state belongs to another source or resource")
		}
		if now <= previous.time {
			return Result{}, fmt.Errorf("sample observation time must advance")
		}
		start = previous.startTime
		if discontinuity(previous, current, values) {
			window = WindowReset
			start = now
			if current.Metadata.CounterStartTime != nil &&
				!proto.Equal(current.Metadata.CounterStartTime, previous.sample.Metadata.CounterStartTime) &&
				reportedStart != previous.startTime {
				start = reportedStart
			}
		} else {
			window = WindowClockUnknown
			if previous.sample.Metadata.TickRateHz != nil && previous.sample.Metadata.SampleTicks != nil &&
				current.Metadata.TickRateHz != nil && current.Metadata.SampleTicks != nil {
				switch {
				case current.Metadata.GetTickRateHz() != previous.sample.Metadata.GetTickRateHz(),
					current.Metadata.GetSampleTicks() < previous.sample.Metadata.GetSampleTicks():
					window = WindowClockChanged
				case current.Metadata.GetSampleTicks() == previous.sample.Metadata.GetSampleTicks():
					window = WindowClockUnchanged
				default:
					window = WindowReady
					if current.Metadata.CounterEpoch == "" && current.Metadata.CounterStartTime == nil {
						window = WindowInferred
					}
				}
			}
		}
	}
	if start > now {
		return Result{}, fmt.Errorf("counter start time is after observation time")
	}
	output := &metrics.ResourceMetrics{
		Resource: &resource.Resource{Attributes: []*common.KeyValue{
			attribute("opi.source.id", current.SourceID),
			attribute("opi.resource.name", current.ResourceName),
		}},
		ScopeMetrics: []*metrics.ScopeMetrics{{
			Scope: &common.InstrumentationScope{Name: "opiproject.otel.opi-storage"},
		}},
	}
	for i, family := range [3]string{"read", "write", "unmap"} {
		output.ScopeMetrics[0].Metrics = append(output.ScopeMetrics[0].Metrics,
			cumulative("opi.storage."+family+".bytes", "By", values.bytes[i], start, now),
			cumulative("opi.storage."+family+".operations", "{operation}", values.ops[i], start, now),
		)
	}
	if window == WindowReady || window == WindowInferred {
		interval := float64(current.Metadata.GetSampleTicks()-previous.sample.Metadata.GetSampleTicks()) /
			float64(current.Metadata.GetTickRateHz())
		idle := true
		for i, family := range [3]string{"read", "write", "unmap"} {
			operations := values.ops[i] - previous.values.ops[i]
			output.ScopeMetrics[0].Metrics = append(output.ScopeMetrics[0].Metrics,
				gauge("opi.storage."+family+".throughput", "By/s",
					float64(values.bytes[i]-previous.values.bytes[i])/interval, now),
				gauge("opi.storage."+family+".iops", "{operation}/s", float64(operations)/interval, now),
			)
			if operations > 0 {
				idle = false
				mean := float64(values.ticks[i]-previous.values.ticks[i]) /
					float64(current.Metadata.GetTickRateHz()) / float64(operations)
				output.ScopeMetrics[0].Metrics = append(output.ScopeMetrics[0].Metrics,
					gauge("opi.storage."+family+".mean_latency", "s", mean, now),
				)
			}
		}
		if idle {
			window = WindowIdle
		}
	}
	next := current
	next.Stats = proto.Clone(current.Stats).(*pb.VolumeStats)
	next.Metadata = proto.Clone(current.Metadata).(*pb.StatsSampleMetadata)
	return Result{
		Metrics: output,
		Next: State{
			sample: next, values: values, time: now, startTime: start,
		},
		IntervalStatus: window,
	}, nil
}

func validate(sample Sample) (uint64, counters, error) {
	if sample.SourceID == "" || sample.ResourceName == "" || sample.Stats == nil || sample.Metadata == nil {
		return 0, counters{}, fmt.Errorf("source, resource, statistics, and metadata are required")
	}
	now, err := timestampNanos(sample.Metadata.CollectionTime)
	if err != nil || now == 0 {
		return 0, counters{}, fmt.Errorf("a valid positive observation timestamp is required")
	}
	if sample.Metadata.TickRateHz != nil && sample.Metadata.GetTickRateHz() <= 0 {
		return 0, counters{}, fmt.Errorf("a reported tick frequency must be positive")
	}
	if sample.Metadata.SampleTicks != nil && sample.Metadata.GetSampleTicks() < 0 {
		return 0, counters{}, fmt.Errorf("reported sample ticks must be nonnegative")
	}
	if sample.Metadata.CounterStartTime != nil {
		start, err := timestampNanos(sample.Metadata.CounterStartTime)
		if err != nil || start > now {
			return 0, counters{}, fmt.Errorf("counter start time must be valid and not after observation")
		}
	}
	value := counters{
		bytes: [3]int64{int64(sample.Stats.GetReadBytesCount()), int64(sample.Stats.GetWriteBytesCount()), int64(sample.Stats.GetUnmapBytesCount())},
		ops:   [3]int64{int64(sample.Stats.GetReadOpsCount()), int64(sample.Stats.GetWriteOpsCount()), int64(sample.Stats.GetUnmapOpsCount())},
		ticks: [3]int64{int64(sample.Stats.GetReadLatencyTicks()), int64(sample.Stats.GetWriteLatencyTicks()), int64(sample.Stats.GetUnmapLatencyTicks())},
	}
	for i := 0; i < 3; i++ {
		if value.bytes[i] < 0 || value.ops[i] < 0 || value.ticks[i] < 0 {
			return 0, counters{}, fmt.Errorf("negative or unsupported counters cannot be exported as measurements")
		}
	}
	return now, value, nil
}

func discontinuity(previous *State, current Sample, values counters) bool {
	if current.Metadata.CounterEpoch != previous.sample.Metadata.CounterEpoch ||
		!proto.Equal(current.Metadata.CounterStartTime, previous.sample.Metadata.CounterStartTime) {
		return true
	}
	for i := 0; i < 3; i++ {
		if values.bytes[i] < previous.values.bytes[i] || values.ops[i] < previous.values.ops[i] ||
			values.ticks[i] < previous.values.ticks[i] {
			return true
		}
	}
	return false
}

func timestampNanos(timestamp *timestamppb.Timestamp) (uint64, error) {
	if timestamp == nil {
		return 0, fmt.Errorf("timestamp is absent")
	}
	if err := timestamp.CheckValid(); err != nil {
		return 0, err
	}
	if timestamp.Seconds < 0 ||
		uint64(timestamp.Seconds) > (math.MaxUint64-uint64(timestamp.Nanos))/1000000000 {
		return 0, fmt.Errorf("timestamp cannot be represented by OTLP Unix nanoseconds")
	}
	return uint64(timestamp.Seconds)*1000000000 + uint64(timestamp.Nanos), nil
}

func attribute(key, value string) *common.KeyValue {
	return &common.KeyValue{Key: key, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: value}}}
}

func cumulative(name, unit string, value int64, start, now uint64) *metrics.Metric {
	return &metrics.Metric{
		Name: name, Unit: unit,
		Data: &metrics.Metric_Sum{Sum: &metrics.Sum{
			AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
			IsMonotonic:            true,
			DataPoints: []*metrics.NumberDataPoint{{
				StartTimeUnixNano: start, TimeUnixNano: now,
				Value: &metrics.NumberDataPoint_AsInt{AsInt: value},
			}},
		}},
	}
}

func gauge(name, unit string, value float64, now uint64) *metrics.Metric {
	return &metrics.Metric{
		Name: name, Unit: unit,
		Data: &metrics.Metric_Gauge{Gauge: &metrics.Gauge{DataPoints: []*metrics.NumberDataPoint{{
			TimeUnixNano: now, Value: &metrics.NumberDataPoint_AsDouble{AsDouble: value},
		}}}},
	}
}

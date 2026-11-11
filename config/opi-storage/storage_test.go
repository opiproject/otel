// SPDX-License-Identifier: Apache-2.0
package storageotel

import (
	"math"
	"testing"
	"time"

	pb "github.com/opiproject/opi-api/storage/v1alpha1/gen/go"
	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sample(at int64, sourceTicks, bytes, operations, latency int64) Sample {
	stats := &pb.VolumeStats{}
	stats.ReadBytesCount = int32(bytes)
	stats.ReadOpsCount = int32(operations)
	stats.ReadLatencyTicks = int32(latency)
	return Sample{
		SourceID: "dpu-1", ResourceName: "aioVolumes/v",
		Stats: stats,
		Metadata: &pb.StatsSampleMetadata{
			CollectionTime: timestamppb.New(time.Unix(at, 0)),
			TickRateHz:     proto.Int64(1000),
			SampleTicks:    proto.Int64(sourceTicks),
		},
	}
}

func metric(t *testing.T, output *metrics.ResourceMetrics, name string) *metrics.Metric {
	t.Helper()
	for _, scope := range output.ScopeMetrics {
		for _, value := range scope.Metrics {
			if value.Name == name {
				return value
			}
		}
	}
	t.Fatalf("metric %q is absent", name)
	return nil
}

func hasMetric(output *metrics.ResourceMetrics, name string) bool {
	for _, scope := range output.ScopeMetrics {
		for _, value := range scope.Metrics {
			if value.Name == name {
				return true
			}
		}
	}
	return false
}

func TestAbsoluteCountersAndIntervalUnits(t *testing.T) {
	firstSample := sample(10, 9007199254740993, 100, 10, 50)
	first, err := Convert(firstSample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.IntervalStatus != WindowFirstSample {
		t.Fatal("the first observation must establish a baseline")
	}
	secondSample := sample(11, 9007199254741993, 120, 12, 70)
	second, err := Convert(secondSample, &first.Next)
	if err != nil {
		t.Fatal(err)
	}
	if second.IntervalStatus != WindowInferred {
		t.Fatal("an unreported source epoch must not become proven continuity")
	}
	sum := metric(t, second.Metrics, "opi.storage.read.bytes").GetSum()
	point := sum.DataPoints[0]
	if point.GetAsInt() != 120 || !sum.IsMonotonic ||
		sum.AggregationTemporality != metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
		t.Fatal("absolute source totals must not be added together as increments")
	}
	if point.StartTimeUnixNano != uint64(10*time.Second) || point.TimeUnixNano != uint64(11*time.Second) {
		t.Fatal("unknown-start cumulative streams must retain their observation baseline")
	}
	if firstPoint := metric(t, first.Metrics, "opi.storage.read.bytes").GetSum().DataPoints[0]; firstPoint.StartTimeUnixNano != firstPoint.TimeUnixNano {
		t.Fatal("first observation with unknown source start needs a zero-duration reset point")
	}
	for _, test := range []struct {
		name string
		unit string
		want float64
	}{
		{"opi.storage.read.throughput", "By/s", 20},
		{"opi.storage.read.iops", "{operation}/s", 2},
		{"opi.storage.read.mean_latency", "s", 0.01},
	} {
		value := metric(t, second.Metrics, test.name)
		if value.Unit != test.unit || math.Abs(value.GetGauge().DataPoints[0].GetAsDouble()-test.want) > 1e-12 {
			t.Errorf("%s lost clock precision or units: %v", test.name, value)
		}
	}
	request := &collector.ExportMetricsServiceRequest{ResourceMetrics: []*metrics.ResourceMetrics{second.Metrics}}
	wire, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded collector.ExportMetricsServiceRequest
	if err := proto.Unmarshal(wire, &decoded); err != nil || !proto.Equal(request, &decoded) {
		t.Fatalf("standard OTLP export shape round trip: %v", err)
	}
}

func TestDiscontinuitiesDoNotProduceRateSpikes(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Sample)
	}{
		{"counter decrease", func(value *Sample) {
			value.Stats.ReadBytesCount = 5
			value.Stats.ReadOpsCount = 1
			value.Stats.ReadLatencyTicks = 5
		}},
		{"epoch change despite growth", func(value *Sample) { value.Metadata.CounterEpoch = "new-generation" }},
		{"reported start changes", func(value *Sample) {
			value.Metadata.CounterStartTime = timestamppb.New(time.Unix(10, 500000000))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := Convert(sample(10, 1000, 100, 10, 50), nil)
			if err != nil {
				t.Fatal(err)
			}
			current := sample(11, 2000, 120, 12, 70)
			test.change(&current)
			output, err := Convert(current, &first.Next)
			if err != nil {
				t.Fatal(err)
			}
			if output.IntervalStatus != WindowReset || hasMetric(output.Metrics, "opi.storage.read.iops") ||
				hasMetric(output.Metrics, "opi.storage.read.mean_latency") {
				t.Fatal("a reset must establish a new baseline, not emit interval metrics")
			}
			if got := metric(t, output.Metrics, "opi.storage.read.bytes").GetSum().DataPoints[0].GetAsInt(); got != int64(current.Stats.ReadBytesCount) {
				t.Fatal("reset handling must not change the reported absolute total")
			}
		})
	}
}

func TestClockAvailabilityAndChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Sample)
		want   WindowStatus
	}{
		{"frequency absent", func(value *Sample) { value.Metadata.TickRateHz = nil }, WindowClockUnknown},
		{"ticks absent", func(value *Sample) { value.Metadata.SampleTicks = nil }, WindowClockUnknown},
		{"frequency changes", func(value *Sample) { value.Metadata.TickRateHz = proto.Int64(2000) }, WindowClockChanged},
		{"source clock restarts", func(value *Sample) { value.Metadata.SampleTicks = proto.Int64(1) }, WindowClockChanged},
		{"duplicate source snapshot", func(value *Sample) { value.Metadata.SampleTicks = proto.Int64(1000) }, WindowClockUnchanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := Convert(sample(10, 1000, 100, 10, 50), nil)
			if err != nil {
				t.Fatal(err)
			}
			current := sample(11, 2000, 120, 12, 70)
			test.change(&current)
			output, err := Convert(current, &first.Next)
			if err != nil {
				t.Fatal(err)
			}
			if output.IntervalStatus != test.want || hasMetric(output.Metrics, "opi.storage.read.mean_latency") {
				t.Fatalf("must not assume clock continuity: %v", output.IntervalStatus)
			}
			if metric(t, output.Metrics, "opi.storage.read.bytes").GetSum().DataPoints[0].StartTimeUnixNano != uint64(10*time.Second) {
				t.Fatal("a clock change alone must not invent an I/O counter reset")
			}
		})
	}
}

func TestIdleWindowDoesNotInventLatency(t *testing.T) {
	first, err := Convert(sample(10, 1000, 100, 10, 50), nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Convert(sample(11, 2000, 100, 10, 50), &first.Next)
	if err != nil {
		t.Fatal(err)
	}
	if output.IntervalStatus != WindowIdle || hasMetric(output.Metrics, "opi.storage.read.mean_latency") {
		t.Fatal("zero operations must not become an artificial zero-latency measurement")
	}
	if metric(t, output.Metrics, "opi.storage.read.iops").GetGauge().DataPoints[0].GetAsDouble() != 0 {
		t.Fatal("a measured idle window should report zero IOPS")
	}
}

func TestInvalidSamplesDoNotAdvanceState(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Sample)
	}{
		{"source absent", func(value *Sample) { value.SourceID = "" }},
		{"resource absent", func(value *Sample) { value.ResourceName = "" }},
		{"statistics absent", func(value *Sample) { value.Stats = nil }},
		{"metadata absent", func(value *Sample) { value.Metadata = nil }},
		{"observation absent", func(value *Sample) { value.Metadata.CollectionTime = nil }},
		{"zero frequency", func(value *Sample) { value.Metadata.TickRateHz = proto.Int64(0) }},
		{"negative frequency", func(value *Sample) { value.Metadata.TickRateHz = proto.Int64(-1) }},
		{"negative ticks", func(value *Sample) { value.Metadata.SampleTicks = proto.Int64(-1) }},
		{"unsupported counter", func(value *Sample) { value.Stats.ReadOpsCount = -1 }},
		{"older observation", func(value *Sample) { value.Metadata.CollectionTime = timestamppb.New(time.Unix(9, 0)) }},
		{"duplicate observation", func(value *Sample) { value.Metadata.CollectionTime = timestamppb.New(time.Unix(10, 0)) }},
		{"source mismatch", func(value *Sample) { value.SourceID = "another-dpu" }},
		{"resource mismatch", func(value *Sample) { value.ResourceName = "aioVolumes/another" }},
		{"future start", func(value *Sample) { value.Metadata.CounterStartTime = timestamppb.New(time.Unix(12, 0)) }},
		{"OTLP timestamp overflow", func(value *Sample) {
			value.Metadata.CollectionTime = &timestamppb.Timestamp{Seconds: int64(math.MaxUint64/1000000000) + 1}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := Convert(sample(10, 1000, 100, 10, 50), nil)
			if err != nil {
				t.Fatal(err)
			}
			current := sample(11, 2000, 120, 12, 70)
			test.change(&current)
			output, err := Convert(current, &first.Next)
			if err == nil || output.Metrics != nil {
				t.Fatal("invalid samples must not produce success-shaped metrics")
			}
			valid, err := Convert(sample(11, 2000, 120, 12, 70), &first.Next)
			if err != nil || valid.IntervalStatus != WindowInferred {
				t.Fatalf("a rejected sample changed previous state: %v", err)
			}
		})
	}
}

func TestStateDoesNotAliasInput(t *testing.T) {
	input := sample(10, 1000, 100, 10, 50)
	first, err := Convert(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	input.Stats.ReadBytesCount = 999
	*input.Metadata.TickRateHz = 1
	input.Metadata.CounterEpoch = "changed"
	next, err := Convert(sample(11, 2000, 120, 12, 70), &first.Next)
	if err != nil || next.IntervalStatus != WindowInferred {
		t.Fatalf("caller mutation changed retained state: %v", err)
	}
}

func TestExplicitCounterStartTime(t *testing.T) {
	current := sample(10, 1000, 100, 10, 50)
	current.Metadata.CounterEpoch = "known-generation"
	current.Metadata.CounterStartTime = timestamppb.New(time.Unix(1, 0))
	output, err := Convert(current, nil)
	if err != nil {
		t.Fatal(err)
	}
	if metric(t, output.Metrics, "opi.storage.read.bytes").GetSum().DataPoints[0].StartTimeUnixNano != uint64(time.Second) {
		t.Fatal("known source start time must be preserved")
	}
	for _, attr := range output.Metrics.Resource.Attributes {
		if attr.Key == "counter_epoch" || attr.Key == "opi.counter.epoch" {
			t.Fatal("reset tokens must not create new resource-label cardinality")
		}
	}
	next := sample(11, 2000, 120, 12, 70)
	next.Metadata.CounterEpoch = current.Metadata.CounterEpoch
	next.Metadata.CounterStartTime = current.Metadata.CounterStartTime
	continuous, err := Convert(next, &output.Next)
	if err != nil || continuous.IntervalStatus != WindowReady {
		t.Fatalf("a matching known epoch should establish continuity: %v", err)
	}
}

func TestObservedResetDoesNotReuseStaleStartTime(t *testing.T) {
	firstSample := sample(10, 1000, 100, 10, 50)
	firstSample.Metadata.CounterStartTime = timestamppb.New(time.Unix(1, 0))
	first, err := Convert(firstSample, nil)
	if err != nil {
		t.Fatal(err)
	}
	reset := sample(11, 2000, 5, 1, 5)
	reset.Metadata.CounterStartTime = firstSample.Metadata.CounterStartTime
	output, err := Convert(reset, &first.Next)
	if err != nil {
		t.Fatal(err)
	}
	point := metric(t, output.Metrics, "opi.storage.read.bytes").GetSum().DataPoints[0]
	if output.IntervalStatus != WindowReset || point.StartTimeUnixNano != point.TimeUnixNano {
		t.Fatal("a detected reset must not remain in the stale cumulative segment")
	}
}

func TestClockChangeRebaselinesNextWindow(t *testing.T) {
	first, err := Convert(sample(10, 1000, 100, 10, 50), nil)
	if err != nil {
		t.Fatal(err)
	}
	changed := sample(11, 3000, 120, 12, 70)
	changed.Metadata.TickRateHz = proto.Int64(2000)
	second, err := Convert(changed, &first.Next)
	if err != nil || second.IntervalStatus != WindowClockChanged {
		t.Fatalf("frequency transition: %v", err)
	}
	stable := sample(12, 5000, 140, 14, 90)
	stable.Metadata.TickRateHz = proto.Int64(2000)
	third, err := Convert(stable, &second.Next)
	if err != nil || third.IntervalStatus != WindowInferred {
		t.Fatalf("stable new-frequency window: %v", err)
	}
	if metric(t, third.Metrics, "opi.storage.read.iops").GetGauge().DataPoints[0].GetAsDouble() != 2 {
		t.Fatal("new-frequency intervals must use the re-established baseline")
	}
}

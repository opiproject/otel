// SPDX-License-Identifier: Apache-2.0
package storageotel_test

import (
	"fmt"
	"time"

	pb "github.com/opiproject/opi-api/storage/v1alpha1/gen/go"
	storageotel "github.com/opiproject/otel/config/opi-storage"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ExampleConvert() {
	response := &pb.StatsAioVolumeResponse{
		Stats: &pb.VolumeStats{ReadBytesCount: 100, ReadOpsCount: 10, ReadLatencyTicks: 50},
		SampleMetadata: &pb.StatsSampleMetadata{
			CollectionTime: timestamppb.New(time.Unix(10, 0)),
			TickRateHz:     proto.Int64(1000), SampleTicks: proto.Int64(1000),
		},
	}
	result, err := storageotel.Convert(storageotel.Sample{
		SourceID: "dpu-1", ResourceName: "aioVolumes/v",
		Stats: response.GetStats(), Metadata: response.GetSampleMetadata(),
	}, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.IntervalStatus)
	fmt.Println(result.Metrics.ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].GetAsInt())
	// Output:
	// first_sample
	// 100
}

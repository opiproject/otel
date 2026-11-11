module github.com/opiproject/otel/config/opi-storage

go 1.24.0

require (
	github.com/opiproject/opi-api v0.0.0-20240415072823-bb755a5f6ecc
	go.opentelemetry.io/proto/otlp v1.5.0
	google.golang.org/protobuf v1.36.4
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.25.1 // indirect
	golang.org/x/net v0.33.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250102185135-69823020774d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250102185135-69823020774d // indirect
	google.golang.org/grpc v1.69.2 // indirect
)

replace github.com/opiproject/opi-api => github.com/singh4jitendra/opi-api v1.0.1-0.20261111190817-1468f9b2a573

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

type Bdev struct {
	Name              string `json:"name"`
	BytesRead         int64  `json:"bytes_read"`
	NumReadOps        int64  `json:"num_read_ops"`
	BytesWritten      int64  `json:"bytes_written"`
	NumWriteOps       int64  `json:"num_write_ops"`
	ReadLatencyTicks  int64  `json:"read_latency_ticks"`
	WriteLatencyTicks int64  `json:"write_latency_ticks"`
}

type SPDKResponse struct {
	Result struct {
		Bdevs []Bdev `json:"bdevs"`
	} `json:"result"`
}

func initProvider() func() {
	ctx := context.Background()

	res, err := resource.New(ctx,

		resource.WithAttributes(
			semconv.ServiceNameKey.String("spdk-client"),
		),
	)
	handleErr(err, "failed to create resource")

	otelAgentAddr := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelAgentAddr == "" {
		otelAgentAddr = "otel-gw-collector:4317"
	}

	// Metric Exporter
	metricExp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(otelAgentAddr),
	)
	handleErr(err, "Failed to create the collector metric exporter")

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)

	// Trace Exporter
	traceClient := otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(otelAgentAddr))
	traceExp, err := otlptrace.New(ctx, traceClient)
	handleErr(err, "Failed to create trace exporter")

	bsp := sdktrace.NewBatchSpanProcessor(traceExp)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func() {
		cxt, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := traceExp.Shutdown(cxt); err != nil {
			log.Printf("Failed to shutdown trace exporter: %v", err)
		}
		if err := meterProvider.Shutdown(cxt); err != nil {
			log.Printf("Failed to shutdown metric exporter: %v", err)
		}
	}
}

func handleErr(err error, message string) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}

func fetchSPDKMetrics(ctx context.Context) []Bdev {
	url := "http://spdk:9009"
	reqBody := []byte(`{"id":1, "method": "bdev_get_iostat"}`)

	client := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.SetBasicAuth("spdkuser", "spdkpass")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	var response SPDKResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Fatalf("Failed to parse SPDK response: %v", err)
	}

	return response.Result.Bdevs
}

func main() {
	shutdown := initProvider()
	defer shutdown()

	tracer := otel.Tracer("spdk-client")
	meter := otel.Meter("spdk-client-meter")

	// Metrics
	bytesRead, _ := meter.Int64Counter("spdk/bdev/bytes_read")
	numReadOps, _ := meter.Int64Counter("spdk/bdev/read_ops")

	for{
		ctx, span := tracer.Start(context.Background(), "FetchSPDKMetrics")
		bdevs := fetchSPDKMetrics(ctx)

		for _, bdev := range bdevs {
			attributes := []attribute.KeyValue{
				attribute.String("bdev.name", bdev.Name),
			}
			bytesRead.Add(ctx, bdev.BytesRead, metric.WithAttributes(attributes...))
			numReadOps.Add(ctx, bdev.NumReadOps, metric.WithAttributes(attributes...))

			fmt.Printf("Bdev: %s, BytesRead: %d, NumReadOps: %d\n",
				bdev.Name, bdev.BytesRead, bdev.NumReadOps)
		}
	    span.End()
	    time.Sleep(5 * time.Second)
    }
	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// Application logic
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, World!")
	})

	// Start server
	http.ListenAndServe(":57400", nil)		
}

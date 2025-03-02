#include <iostream>
#include <vector>
#include <string>
#include <cstring>
#include <cstdlib>
#include <cstdint>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/stat.h>
#include <unistd.h>
#include <doca_argp.h>
#include <doca_log.h>
#include <doca_telemetry.h>
#include "opentelemetry/exporters/otlp/otlp_http_exporter_factory.h"
#include "opentelemetry/exporters/otlp/otlp_http_exporter_options.h"
#include "opentelemetry/context/propagation/global_propagator.h"
#include "opentelemetry/context/propagation/text_map_propagator.h"
#include "opentelemetry/exporters/ostream/span_exporter_factory.h"
#include "opentelemetry/nostd/shared_ptr.h"
#include "opentelemetry/sdk/trace/simple_processor_factory.h"
#include "opentelemetry/sdk/trace/tracer_context.h"
#include "opentelemetry/sdk/trace/tracer_context_factory.h"
#include "opentelemetry/sdk/trace/tracer_provider_factory.h"
#include "opentelemetry/trace/propagation/http_trace_context.h"
#include "opentelemetry/trace/provider.h"
#include "opentelemetry/ext/http/client/http_client_factory.h"
#include "opentelemetry/sdk/resource/semantic_conventions.h"
#include "opentelemetry/sdk/common/global_log_handler.h"

#define NB_EXAMPLE_STRINGS 5        /* Amount of example strings */
#define MAX_EXAMPLE_STRING_SIZE 256 /* Indicates the max length of string */
#define SINGLE_FIELD_VALUE 1        /* Indicates the field contains one value */
namespace trace     = opentelemetry::trace;
namespace nostd = opentelemetry::nostd;
namespace trace_sdk = opentelemetry::sdk::trace;
namespace otlp      = opentelemetry::exporter::otlp;
namespace internal_log = opentelemetry::sdk::common::internal_log;
namespace resource = opentelemetry::sdk::resource;

opentelemetry::nostd::shared_ptr<opentelemetry::trace::Tracer> getTracer(
    std::string serviceName)
{
    auto provider = opentelemetry::trace::Provider::GetTracerProvider();
    return provider->GetTracer(serviceName);
}
    opentelemetry::exporter::otlp::OtlpHttpExporterOptions opts;


void traces(std::string serviceName, std::string operationName)
{
    auto span = getTracer(serviceName)->StartSpan(operationName);

    span->SetAttribute("service.name", serviceName);

    span->End();
}

void InitTracer()
    {
        // Create an OpenTelemetry Protocol (OTLP) exporter.
        auto exporter  = otlp::OtlpHttpExporterFactory::Create(opts);
        auto processor = trace_sdk::SimpleSpanProcessorFactory::Create(std::move(exporter));

        resource::ResourceAttributes attributes = {
                {resource::SemanticConventions::kServiceName, "DOCA NVIDIA DEMO"}, // The application name.
                {resource::SemanticConventions::kHostName, "$"}
        };
        auto resource = opentelemetry::sdk::resource::Resource::Create(attributes);

        std::shared_ptr<opentelemetry::trace::TracerProvider> provider =
                trace_sdk::TracerProviderFactory::Create(std::move(processor), std::move(resource));

        // Set the trace provider.
        trace::Provider::SetTracerProvider(provider);
    }

    void CleanupTracer()
    {
        std::shared_ptr<opentelemetry::trace::TracerProvider> none;
        trace::Provider::SetTracerProvider(none);
    }

    nostd::shared_ptr<trace::Tracer> get_tracer()
    {
        auto provider = trace::Provider::GetTracerProvider();
        return provider->GetTracer("library name to trace", OPENTELEMETRY_SDK_VERSION);
    }
DOCA_LOG_REGISTER(TELEMETRY);

static std::vector<std::string> example_strings = {
    "example_str_1", "example_str_2", "example_str_3", "example_str_4", "example_str_5"
};

/* Event struct from which report will be serialized */
struct TestEventType {
    doca_telemetry_timestamp_t timestamp;
    int32_t event_number;
    int32_t iter_number;
    uint64_t string_number;
    char example_string[MAX_EXAMPLE_STRING_SIZE];
} __attribute__((packed));

/*
 * This function fills up event buffer with the example string of specified number.
 * It also saves number of iteration, number of string and overall number of events.
 */
static doca_error_t prepare_example_event(int32_t iter_number, uint64_t string_number, TestEventType &event) {
    static int collected_example_events_count = 0;
    doca_telemetry_timestamp_t timestamp;
    doca_error_t result = DOCA_SUCCESS;
              traces("doca", "doca prepare");
result = doca_telemetry_get_timestamp(&timestamp);

    if (result != DOCA_SUCCESS)
        return result;

    event.timestamp = timestamp;
    event.event_number = collected_example_events_count++;
    event.iter_number = iter_number;
    event.string_number = string_number;

    if (example_strings[string_number].length() >= MAX_EXAMPLE_STRING_SIZE)
        return DOCA_ERROR_INVALID_VALUE;

    strncpy(event.example_string, example_strings[string_number].c_str(), MAX_EXAMPLE_STRING_SIZE);
    return DOCA_SUCCESS;
}

/*
 * Registers the example fields to the doca_telemetry_type.
 */
static doca_error_t telemetry_register_fields(doca_telemetry_type *type) {
         doca_error_t result;
    doca_telemetry_field *field;
    const int nb_fields = 5;

    struct {
        const char *name;
        const char *desc;
        const char *type_name;
        uint16_t len;
    } fields_info[] = {
        {"timestamp", "Event timestamp", DOCA_TELEMETRY_FIELD_TYPE_TIMESTAMP, SINGLE_FIELD_VALUE},
        {"event_number", "Event number", DOCA_TELEMETRY_FIELD_TYPE_INT32, SINGLE_FIELD_VALUE},
        {"iter_num", "Iteration number", DOCA_TELEMETRY_FIELD_TYPE_INT32, SINGLE_FIELD_VALUE},
        {"string_number", "String number", DOCA_TELEMETRY_FIELD_TYPE_UINT64, SINGLE_FIELD_VALUE},
        {"example_string", "String example", DOCA_TELEMETRY_FIELD_TYPE_CHAR, MAX_EXAMPLE_STRING_SIZE},
    };

    for (int i = 0; i < nb_fields; i++) {
        result = doca_telemetry_field_create(&field);
        if (result != DOCA_SUCCESS)
            return result;
        traces("register fields", fields_info[i].name);

        doca_telemetry_field_set_name(field, fields_info[i].name);
        doca_telemetry_field_set_description(field, fields_info[i].desc);
        doca_telemetry_field_set_type_name(field, fields_info[i].type_name);
        doca_telemetry_field_set_array_len(field, fields_info[i].len);

        result = doca_telemetry_type_add_field(type, field);
        if (result != DOCA_SUCCESS)
            return result;
    }

    return result;
}

doca_error_t telemetry_export() {
    bool file_write_enable = true;
    bool ipc_enabled = true;
    int repetition = 10;
    doca_error_t result;
    int32_t iteration = 0;
    uint64_t string_number = 0;
    doca_telemetry_schema *doca_schema = nullptr;
    doca_telemetry_source *doca_source = nullptr;
    TestEventType test_event;
    doca_telemetry_type_index_t example_index;
    doca_telemetry_type *example_type;

    result = doca_telemetry_schema_init("example_doca_schema_name", &doca_schema);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot init doca schema");
        return result;
    }

    doca_telemetry_schema_set_buf_size(doca_schema, sizeof(test_event) * 5);

    result = doca_telemetry_type_create(&example_type);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot create type");
        goto err_schema;
    }

    result = telemetry_register_fields(example_type);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot register fields");
        doca_telemetry_type_destroy(example_type);
        goto err_schema;
    }

    if (file_write_enable)
        doca_telemetry_schema_set_file_write_enabled(doca_schema);

    if (ipc_enabled)
        doca_telemetry_schema_set_ipc_enabled(doca_schema);

    result = doca_telemetry_schema_add_type(doca_schema, "example_event", example_type, &example_index);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot add type to doca_schema!");
        goto err_schema;
    }

    result = doca_telemetry_schema_start(doca_schema);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot start doca_schema!");
        goto err_schema;
    }

    result = doca_telemetry_source_create(doca_schema, &doca_source);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot create doca_source!");
        goto err_schema;
    }

    doca_telemetry_source_set_id(doca_source, "source_1");
    doca_telemetry_source_set_tag(doca_source, "source_1_tag");

    result = doca_telemetry_source_start(doca_source);
    if (result != DOCA_SUCCESS) {
        DOCA_LOG_ERR("Cannot start doca_source!");
        goto err_source;
    }

    for (iteration = 0; iteration < repetition; iteration++) {
        for (string_number = 0; string_number < NB_EXAMPLE_STRINGS; string_number++) {
            result = prepare_example_event(iteration, string_number,  test_event);
            if (result != DOCA_SUCCESS) {
                DOCA_LOG_ERR("Failed to create event");
                goto err_source;
            }

            result = doca_telemetry_source_report(doca_source, example_index, &test_event, 1);
            if (result != DOCA_SUCCESS) {
                DOCA_LOG_ERR("Cannot report to doca_source!");
                goto err_source;

            }

      if (iteration % 2 == 0) {
                        /*
                         * Optionally force DOCA Telemetry source buffer to flush.
                         * Handy for bursty events or specific event types.
                         */
                        result = doca_telemetry_source_flush(doca_source);
                        if (result != DOCA_SUCCESS) {
                                DOCA_LOG_ERR("Cannot flush doca_source!");
                                goto err_source;
                        }
                }
        }

    }

    doca_telemetry_source_destroy(doca_source);
    doca_telemetry_schema_destroy(doca_schema);
    return DOCA_SUCCESS;

err_source:
    doca_telemetry_source_destroy(doca_source);
err_schema:
    doca_telemetry_schema_destroy(doca_schema);
    return result;
}

int main(int argc, char *argv[]) {
    InitTracer();

        doca_error_t result;
        struct doca_log_backend *sdk_log;
        int exit_status = EXIT_FAILURE;

        /* Register a logger backend */
        result = doca_log_backend_create_standard();
        if (result != DOCA_SUCCESS)
                goto sample_exit;

        /* Register a logger backend for internal SDK errors and warnings */
        result = doca_log_backend_create_with_file_sdk(stderr, &sdk_log);
        if (result != DOCA_SUCCESS)
                goto sample_exit;
        result = doca_log_backend_set_sdk_level(sdk_log, DOCA_LOG_LEVEL_WARNING);
        if (result != DOCA_SUCCESS)
                goto sample_exit;

        DOCA_LOG_INFO("Starting the sample");

        /* Parse cmdline/json arguments */
        result = doca_argp_init("doca_telemetry_export", NULL);
        if (result != DOCA_SUCCESS) {
                DOCA_LOG_ERR("Failed to init ARGP resources: %s", doca_error_get_descr(result));
                goto sample_exit;
        }
        result = doca_argp_start(argc, argv);
        if (result != DOCA_SUCCESS) {
                DOCA_LOG_ERR("Failed to parse sample input: %s", doca_error_get_descr(result));
                goto argp_cleanup;
        }
        result = telemetry_export();
        if (result != DOCA_SUCCESS) {
                DOCA_LOG_ERR("telemetry_export() encountered an error: %s", doca_error_get_descr(result));
                goto argp_cleanup;
        }

        exit_status = EXIT_SUCCESS;

argp_cleanup:
        doca_argp_destroy();
sample_exit:
        if (exit_status == EXIT_SUCCESS)
                DOCA_LOG_INFO("Sample finished successfully");
        else
                DOCA_LOG_INFO("Sample finished with errors");
            CleanupTracer();

        return exit_status;

}

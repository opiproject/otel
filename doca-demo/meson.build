#
# Copyright (c) 2022-2024 NVIDIA CORPORATION & AFFILIATES, ALL RIGHTS RESERVED.
#
# This software product is a proprietary product of NVIDIA CORPORATION &
# AFFILIATES (the "Company") and all right, title, and interest in and to the
# software product, including all associated intellectual property rights, are
# and shall remain exclusively with the Company.
#
# This software product is governed by the End User License Agreement
# provided with the software product
#

project('DOCA_SAMPLE', 'C', 'CPP',
        # Get version number from file.
        version: run_command(find_program('cat'),
                files('/opt/mellanox/doca/applications/VERSION'), check: true).stdout().strip(),
        license: 'Proprietary',
        default_options: ['buildtype=debug'],
        meson_version: '>= 0.61.2'
)

SAMPLE_NAME = 'telemetry_export'

# Comment this line to restore warnings of experimental DOCA features
add_project_arguments('-D DOCA_ALLOW_EXPERIMENTAL_API', language: ['c', 'cpp'])

sample_dependencies = []
# Required for all DOCA programs
sample_dependencies += dependency('doca-common')
# The DOCA library of the sample itself
sample_dependencies += dependency('doca-telemetry')
# Utility DOCA library for executables
sample_dependencies += dependency('doca-argp')
sample_dependencies += dependency('libcurl')
     sample_dependencies +=dependency('opentelemetry-cpp', modules: [
        'opentelemetry-cpp::proto',
        'opentelemetry-cpp::api',
        'opentelemetry-cpp::sdk',
        'opentelemetry-cpp::common',
        'opentelemetry-cpp::trace',
        'opentelemetry-cpp::metrics',
        'opentelemetry-cpp::logs',
        'opentelemetry-cpp::version',
        'opentelemetry-cpp::resources',
        'opentelemetry-cpp::ext',
        'opentelemetry-cpp::http_client_curl',
        'opentelemetry-cpp::otlp_recordable',
        'opentelemetry-cpp::otlp_http_client',
        'opentelemetry-cpp::otlp_http_exporter',
        'opentelemetry-cpp::otlp_http_log_record_exporter',
        'opentelemetry-cpp::otlp_http_metric_exporter',
        'opentelemetry-cpp::ostream_span_exporter',
        'opentelemetry-cpp::ostream_metrics_exporter',
        'opentelemetry-cpp::ostream_log_record_exporter',
        'opentelemetry-cpp::in_memory_span_exporter',
        'opentelemetry-cpp::in_memory_metric_exporter'
    ])
sample_srcs = [
        # Main function for the executable
        'main.cc',
]

sample_inc_dirs  = []

executable('doca_' + SAMPLE_NAME, sample_srcs,
        dependencies : sample_dependencies,
        include_directories: sample_inc_dirs,
        install: false)

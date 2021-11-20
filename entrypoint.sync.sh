#!/bin/env sh

FLAGS=(
# Sync CLI ARGS
    "apikey"
    "ip-address-enabled"
    "timeout-ms"
    "snapshot"
    "force-fresh-startup"
    "storage-type"
    "split-refresh-rate-ms"
    "segment-refresh-rate-ms"
    "impressions-mode"
    "streaming-enabled"
    "http-timeout-ms"
    "internal-metrics-rate-ms"
    "telemetry-push-rate-ms"
    "impressions-fetch-size"
    "impressions-process-concurrency"
    "impressions-process-batch-size"
    "impressions-post-concurrency"
    "impressions-post-size"
    "impressions-accum-wait-ms"
    "events-fetch-size"
    "events-process-concurrency"
    "events-process-batch-size"
    "events-post-concurrency"
    "events-post-size"
    "events-accum-wait-ms"
    "redis-host"
    "redis-port"
    "redis-db"
    "redis-pass"
    "redis-prefix"
    "redis-network"
    "redis-max-retries"
    "redis-dial-timeout"
    "redis-read-timeout"
    "redis-write-timeout"
    "redis-pool"
    "redis-sentinel-replication"
    "redis-sentinel-addresses"
    "redis-sentinel-master"
    "redis-cluster-mode"
    "redis-cluster-nodes"
    "redis-cluster-key-hashtag"
    "redis-tls"
    "redis-tls-server-name"
    "redis-tls-ca-certs"
    "redis-tls-skip-name-validation"
    "redis-tls-client-certificate"
    "redis-tls-client-key"
    "storage-check-rate-ms"

# Common CLI ARGS
    "log-level"
    "log-output"
    "log-rotation-max-files"
    "log-rotation-max-size-kb"
    "admin-host"
    "admin-port"
    "admin-username"
    "admin-password"
    "admin-secure-hc"
    "impression-listener-endpoint"
    "impression-listener-queue-size"
    "slack-webhook"
    "slack-channel"
)

source functions.sh
cli_args=$(parse_env "SPLIT_SYNC" "${FLAGS[@]}")
echo $cli_args
split-sync $cli_args
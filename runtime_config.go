package main

import "time"

const spawnTimeoutVar = "SMT_SPAWN_TIMEOUT_MS"
const evalTimeoutVar = "SMT_EVAL_TIMEOUT_MS"
const internalHTTPMaxRequestBytesVar = "SMT_INTERNAL_HTTP_MAX_BYTES"
const natsMaxCallBytesVar = "SMT_NATS_MAX_CALL_BYTES"
const sourceBundleMaxBytesVar = "SMT_SOURCE_BUNDLE_MAX_BYTES"
const sourceBundleMaxFilesVar = "SMT_SOURCE_BUNDLE_MAX_FILES"

const internalHTTPDefaultMaxRequestBytes = 320 * 1024 * 1024
const natsDefaultMaxCallBytes = 1024 * 1024
const sourceBundleDefaultMaxBytes = 256 * 1024 * 1024
const sourceBundleDefaultMaxFiles = 10000
const evalDefaultTimeout = 30 * time.Second

func spawnTimeout() time.Duration {
	fallback := natsStartupTimeout() + 15*time.Second
	return envDuration(spawnTimeoutVar, fallback, time.Millisecond)
}

func evalTimeout() time.Duration {
	return envDuration(evalTimeoutVar, evalDefaultTimeout, time.Millisecond)
}

func internalHTTPMaxRequestBytes() int64 {
	n := envInt(internalHTTPMaxRequestBytesVar, internalHTTPDefaultMaxRequestBytes)
	if n <= 0 {
		return internalHTTPDefaultMaxRequestBytes
	}
	return int64(n)
}

func natsMaxCallBytes() int {
	n := envInt(natsMaxCallBytesVar, natsDefaultMaxCallBytes)
	if n <= 0 {
		return natsDefaultMaxCallBytes
	}
	return n
}

func sourceBundleMaxBytes() int {
	n := envInt(sourceBundleMaxBytesVar, sourceBundleDefaultMaxBytes)
	if n <= 0 {
		return sourceBundleDefaultMaxBytes
	}
	return n
}

func sourceBundleMaxFiles() int {
	n := envInt(sourceBundleMaxFilesVar, sourceBundleDefaultMaxFiles)
	if n <= 0 {
		return sourceBundleDefaultMaxFiles
	}
	return n
}

// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errors

// User errors (CategoryUser → exit 1)
const (
	CodeCommandNotFound    = "COMMAND_NOT_FOUND"
	CodeParamRequired      = "PARAM_REQUIRED"
	CodeParamInvalid       = "PARAM_INVALID"
	CodeInvalidParams      = "INVALID_PARAMS_JSON"
	CodeOperationCancelled = "OPERATION_CANCELLED"
)

// Server errors (CategoryServer → exit 2)
const (
	CodeServerError   = "SERVER_ERROR"
	CodeNetworkError  = "NETWORK_ERROR"
	CodeServerTimeout = "SERVER_TIMEOUT"
)

// Auth errors (CategoryAuth → exit 3)
const (
	CodeAuthRequired     = "AUTH_REQUIRED"
	CodeAuthFailed       = "AUTH_FAILED"
	CodeTokenExpired     = "TOKEN_EXPIRED"
	CodeRefreshFailed    = "REFRESH_FAILED"
	CodePermissionDenied = "PERMISSION_DENIED"
)

// Config errors (CategoryConfig → exit 4)
const (
	CodeNotInitialized  = "NOT_INITIALIZED"
	CodeConfigInvalid   = "CONFIG_INVALID"
	CodeProfileNotFound = "PROFILE_NOT_FOUND"
)

// Internal errors (CategoryInternal → exit 5)
const (
	CodeRegistryBuildFailed = "REGISTRY_BUILD_FAILED"
	CodeFormatterError      = "FORMATTER_ERROR"
	CodeInternal            = "INTERNAL"
)

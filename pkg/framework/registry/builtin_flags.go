// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

var BuiltinFlags = []FlagDef{
	{Name: "set", Type: FlagTypeStringSlice, Description: "Set nested parameters using key=value, supports dot path."},
	{Name: "params", Short: "P", Type: FlagTypeString, Description: "JSON object whose top-level keys are merged as CLI flags."},
	{Name: "input-file", Type: FlagTypeStringArray, Description: "Read a UTF-8 JSON parameter object from @path (once, max 32 MiB); conflicts with --params."},
	{Name: "output-file", Type: FlagTypeStringArray, Description: "Save selected fields as JSON: {\"path\":\"result.json\",\"fields\":[\"data.items\"]}; accepts an array or repeated objects, never overwrites."},
	{Name: "dry-run", Type: FlagTypeBool, Description: "Render the normalized request without executing the backend."},
	{Name: "format", Short: "o", Type: FlagTypeString, Enum: []string{"json", "ndjson", "table"}, Description: "Output format. Default json."},
	{Name: "select", Type: FlagTypeString, Description: "Field projection; comma-separated dot paths, e.g. id,creator.email."},
	{Name: "envelope", Type: FlagTypeBool, Description: "Wrap result in {data, meta, error} envelope."},
	{Name: "verbose", Short: "v", Type: FlagTypeBool, Description: "Show more columns in --format table."},
	{Name: "auto-paginate", Type: FlagTypeBool, Description: "Automatically fetch and merge all pages when the response contains pagination signals (next_page_token or pagination.has_more)."},
}

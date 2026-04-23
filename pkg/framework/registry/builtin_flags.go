// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package registry

var BuiltinFlags = []FlagDef{
	{Name: "set", Type: FlagTypeStringSlice, Description: "Set nested parameters using key=value, supports dot path."},
	{Name: "params", Short: "P", Type: FlagTypeString, Description: "JSON parameters merged before --set."},
	{Name: "dry-run", Type: FlagTypeBool, Description: "Render the normalized request without executing the backend."},
	{Name: "format", Short: "o", Type: FlagTypeString, Enum: []string{"json", "ndjson", "table"}, Description: "Output format. Default json."},
	{Name: "select", Type: FlagTypeString, Description: "Field projection; comma-separated dot paths, e.g. id,creator.email."},
	{Name: "envelope", Type: FlagTypeBool, Description: "Wrap result in {data, meta, error} envelope."},
	{Name: "verbose", Short: "v", Type: FlagTypeBool, Description: "Show more columns in --format table."},
}

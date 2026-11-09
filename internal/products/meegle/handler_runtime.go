// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package meegle

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
	frameworkerrors "github.com/larksuite/meegle-cli/pkg/framework/errors"
	"github.com/larksuite/meegle-cli/pkg/framework/executor"
	"github.com/larksuite/meegle-cli/pkg/framework/jsonvalue"
	frameworkoutput "github.com/larksuite/meegle-cli/pkg/framework/output"
	"github.com/larksuite/meegle-cli/pkg/framework/pipeline"
	"github.com/larksuite/meegle-cli/pkg/framework/registry"
	"github.com/larksuite/meegle-cli/pkg/runtime/cliapp"
)

// ExecutorKindHandler explicitly routes a command's HandlerRef to ProductConfig.Handlers.
const ExecutorKindHandler = "handler"

type handlerRuntime struct {
	executor    *executor.DirectExecutor
	globalFlags []registry.FlagDef
}

func (runtime *handlerRuntime) Validate(state *pipeline.PipelineContext) error {
	// Global flags live on Cobra's root, outside the registry parent chain.
	// Include them in validation without changing the executable command node.
	parsed, node, validation := *state.Parsed, *state.Parsed.Node, *state
	node.Flags = append(append([]registry.FlagDef(nil), node.Flags...), runtime.globalFlags...)
	parsed.Node = &node
	validation.Parsed = &parsed
	state = &validation
	normalizeStructuredFlagNames(state)
	flags := append(state.Parsed.Node.InheritedFlags(), state.Parsed.Node.Flags...)
	known := make(map[string]struct{}, len(flags)+len(registry.BuiltinFlags))
	for _, flag := range flags {
		known[flag.Name] = struct{}{}
	}
	for _, flag := range registry.BuiltinFlags {
		known[flag.Name] = struct{}{}
	}
	var unknown []string
	for key := range state.Parsed.ExplicitFlags {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return meerrors.NewClientError("CLIENT_INVALID_VALUE", "unknown parameters: "+strings.Join(unknown, ", "))
	}
	if err := validateMeegleCommand(state); err != nil {
		return err
	}
	if err := (&pipeline.ParamValidateStep{}).Execute(context.Background(), state); err != nil {
		return err
	}
	// Structured parameters bypass Cobra's flag parsers. Apply the declared
	// types before dispatch, using the same exact-number JSON representation.
	for _, flag := range flags {
		value, exists := state.Parsed.Flags[flag.Name]
		if _, explicit := state.Parsed.ExplicitFlags[flag.Name]; !exists || !explicit {
			continue
		}
		value, err := handlerFlagValue(flag.Type, value)
		if err != nil {
			return meerrors.NewClientError("CLIENT_INVALID_VALUE", fmt.Sprintf("invalid value for --%s (%s)", flag.Name, flag.Type))
		}
		state.Parsed.Flags[flag.Name] = value
	}
	return nil
}

func (*handlerRuntime) PrepareSession(ctx context.Context, _ *pipeline.PipelineContext) error {
	return ctx.Err()
}

func (runtime *handlerRuntime) Execute(ctx context.Context, state *pipeline.PipelineContext) (err error) {
	// The callback and result serialization are synchronous: no orphan worker
	// is created. Handlers must cooperate with cancellation and own cleanup.
	defer func() {
		if recover() != nil {
			state.Result = nil
			err = meerrors.NewClientError("CLIENT_HANDLER_FAILED", "command handler failed")
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := (&pipeline.ExecutorStep{Executor: runtime.executor}).Execute(ctx, state); err != nil {
		return handlerExecutionError(err)
	}
	result := state.Result
	state.Result = nil
	if err := ctx.Err(); err != nil {
		return err
	}
	if result == nil {
		return meerrors.NewClientError("CLIENT_HANDLER_INVALID_RESULT", "command handler returned no result")
	}
	if result.Err != nil {
		return handlerExecutionError(result.Err)
	}
	// Freeze JSON data and metadata once, before the shared output hooks. This
	// contains custom marshalers and avoids invoking them again while rendering.
	data, err := json.Marshal(result)
	if err != nil {
		return meerrors.NewClientError("CLIENT_HANDLER_INVALID_RESULT", "command handler result is not JSON serializable")
	}
	var snapshot executor.RawResult
	if err := jsonvalue.Unmarshal(data, &snapshot); err != nil {
		return meerrors.NewClientError("CLIENT_HANDLER_INVALID_RESULT", "command handler result is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state.Result = &snapshot
	return nil
}

func handlerExecutionError(err error) error {
	if nilProductDependency(err) || cliapp.IsSuccessfulExit(err) {
		return meerrors.NewClientError("CLIENT_HANDLER_FAILED", "command handler returned an invalid error")
	}
	// Freeze extension-supplied error payloads while the runtime's panic guard
	// is active. Rendering later must not call custom JSON marshalers again.
	data, marshalErr := json.Marshal(frameworkoutput.BuildErrorEnvelope(err, nil))
	if marshalErr != nil {
		return meerrors.NewClientError("CLIENT_HANDLER_FAILED", "command handler returned an invalid error")
	}
	var snapshot struct {
		Error map[string]any
		Meta  map[string]any
	}
	if decodeErr := jsonvalue.Unmarshal(data, &snapshot); decodeErr != nil {
		return meerrors.NewClientError("CLIENT_HANDLER_FAILED", "command handler returned an invalid error")
	}
	return &handlerError{cause: err, message: frameworkerrors.SafeMessage(err, "command handler failed"), payload: snapshot.Error, metadata: snapshot.Meta}
}

// handlerError keeps error identity and exit classification through a guarded
// cause, with inert JSON payloads for the shared error renderer.
type handlerError struct {
	cause    error
	message  string
	payload  map[string]any
	metadata map[string]any
}

func (err *handlerError) Error() string                 { return err.message }
func (err *handlerError) Unwrap() error                 { return frameworkerrors.GuardCause(err.cause) }
func (err *handlerError) ErrorPayload() map[string]any  { return err.payload }
func (err *handlerError) ErrorMetadata() map[string]any { return err.metadata }

func handlerFlagValue(kind string, value any) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("null parameter")
	}
	var target any
	switch kind {
	case registry.FlagTypeString:
		target = new(string)
	case registry.FlagTypeBool:
		target = new(bool)
	case registry.FlagTypeInt:
		target = new(int)
	case registry.FlagTypeFloat:
		target = new(float64)
	case registry.FlagTypeNumber, registry.FlagTypeInteger:
		number, err := jsonvalue.ParseNumber(fmt.Sprint(value))
		if err != nil || (kind == registry.FlagTypeInteger && !jsonvalue.IsInteger(number)) {
			return nil, fmt.Errorf("invalid number")
		}
		return number, nil
	case registry.FlagTypeStringSlice, registry.FlagTypeStringArray:
		target = new([]string)
	case registry.FlagTypeIntSlice:
		target = new([]int)
	case registry.FlagTypeIntegerSlice:
		target = new([]json.Number)
	case registry.FlagTypeObject:
		target = new(map[string]any)
	case registry.FlagTypeObjectSlice:
		target = new([]map[string]any)
	default:
		return value, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if raw, ok := value.(string); ok && (kind == registry.FlagTypeObject || kind == registry.FlagTypeObjectSlice) {
		data = []byte(raw)
	}
	if err := jsonvalue.Unmarshal(data, target); err != nil {
		return nil, err
	}
	if numbers, ok := target.(*[]json.Number); ok {
		for _, number := range *numbers {
			if !jsonvalue.IsInteger(number) {
				return nil, fmt.Errorf("invalid integer")
			}
		}
	}
	// Keep the standard Go types exposed by the framework's flag parser.
	return reflect.ValueOf(target).Elem().Interface(), nil
}

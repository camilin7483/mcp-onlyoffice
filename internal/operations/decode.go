package operations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

const (
	// MaxOperationBytes caps a single operation request (64 KiB).
	MaxOperationBytes = 64 * 1024
	// MaxParamsKeys caps the number of param keys per operation.
	MaxParamsKeys = 64
	// MaxOperationNameLen caps the operation name length.
	MaxOperationNameLen = 128
)

var (
	paramKeyPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,63}$`)
	// Dotted capability-style names; registry membership (unknown names)
	// is checked via Registry.Lookup, not here.
	operationNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*$`)
)

// DecodeOperation parses and strictly validates one operation request.
// Errors name the offending field and never echo params data.
func DecodeOperation(data []byte) (Operation, error) {
	var op Operation
	if len(data) > MaxOperationBytes {
		return Operation{}, fmt.Errorf("operation: request exceeds %d byte limit", MaxOperationBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&op); err != nil {
		return Operation{}, fmt.Errorf("operation: invalid JSON: %v", err)
	}
	if dec.More() {
		return Operation{}, fmt.Errorf("operation: unexpected trailing data")
	}
	if len(op.Name) == 0 || len(op.Name) > MaxOperationNameLen || !operationNamePattern.MatchString(op.Name) {
		return Operation{}, fmt.Errorf("operation: unknown or empty operation name")
	}
	if op.Target == (Target{}) {
		return Operation{}, fmt.Errorf("target: empty target")
	}
	if op.Target.Paragraph != nil && (op.Target.RangeStart != nil || op.Target.RangeEnd != nil) {
		return Operation{}, fmt.Errorf("target: paragraph and range_start/range_end cannot be combined")
	}
	if op.Target.Paragraph != nil && *op.Target.Paragraph < 0 {
		return Operation{}, fmt.Errorf("target: paragraph must not be negative")
	}
	if op.Target.RangeStart != nil && *op.Target.RangeStart < 0 {
		return Operation{}, fmt.Errorf("target: range_start must not be negative")
	}
	if op.Target.RangeEnd != nil && *op.Target.RangeEnd < 0 {
		return Operation{}, fmt.Errorf("target: range_end must not be negative")
	}
	if op.Target.RangeStart != nil && op.Target.RangeEnd != nil && *op.Target.RangeStart > *op.Target.RangeEnd {
		return Operation{}, fmt.Errorf("target: range_start must not exceed range_end")
	}
	if len(op.Params) > MaxParamsKeys {
		return Operation{}, fmt.Errorf("params: too many entries (max %d)", MaxParamsKeys)
	}
	for k := range op.Params {
		if !paramKeyPattern.MatchString(k) {
			return Operation{}, fmt.Errorf("params: invalid key format")
		}
	}
	return op, nil
}

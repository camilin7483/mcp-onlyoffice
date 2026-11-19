package plugin

// Typed errors for the versioned plugin bridge.
//
// Every failure to talk to a desktop editor surfaces as an *RPCError so
// callers can distinguish "the model asked for something the editor cannot
// do" (unsupported_capability, never retry) from "the bytes may already be
// on the wire" (outcome_unknown, must not blindly retry a mutation).
// Message keeps the legacy human-readable phrasing ("plugin not connected",
// "plugin disconnected", "timeout waiting for plugin response", ...)
// because existing tool handlers match on that text.
const (
	CodePluginNotConnected    = "plugin_not_connected"
	CodeSessionNotFound       = "session_not_found"
	CodeUnsupportedCapability = "unsupported_capability"
	CodeProtocolMismatch      = "protocol_mismatch"
	CodeInvalidFrame          = "invalid_frame"
	CodeTimeout               = "timeout"
	CodeOutcomeUnknown        = "outcome_unknown"
	CodePluginError           = "plugin_error"
)

// RPCError is the typed bridge failure. Implement on *RPCError.
type RPCError struct {
	Code           string
	Message        string
	Retriable      bool
	OutcomeUnknown bool
}

func (e *RPCError) Error() string {
	if e == nil {
		return "<nil rpc error>"
	}
	if e.Code == "" {
		return e.Message
	}
	return "[" + e.Code + "] " + e.Message
}

func errNotConnected() *RPCError {
	return &RPCError{Code: CodePluginNotConnected, Message: "plugin not connected", Retriable: true}
}

func errSessionNotFound(id string) *RPCError {
	return &RPCError{Code: CodeSessionNotFound, Message: "session not found: " + id}
}

func errUnsupported(sessionID, editor, method, cap string) *RPCError {
	return &RPCError{Code: CodeUnsupportedCapability,
		Message: "session " + sessionID + " (" + editor + ") does not advertise capability " + cap + " required by method " + method}
}

func errTooManyPending() *RPCError {
	return &RPCError{Code: CodePluginError, Message: "too many pending plugin requests (32)"}
}

func errTimeout(msg string) *RPCError {
	return &RPCError{Code: CodeTimeout, Message: msg, Retriable: true}
}

func errOutcomeUnknown(msg string) *RPCError {
	return &RPCError{Code: CodeOutcomeUnknown, Message: msg, OutcomeUnknown: true}
}

func errPluginError(msg string) *RPCError {
	return &RPCError{Code: CodePluginError, Message: msg}
}

// errDisconnected fails pending requests when their connection drops. The
// command bytes were already written, so the mutation may have applied:
// OutcomeUnknown is true. The message keeps the legacy "plugin
// disconnected" text.
func errDisconnected() *RPCError {
	return &RPCError{Code: CodePluginNotConnected, Message: "plugin disconnected", Retriable: true, OutcomeUnknown: true}
}

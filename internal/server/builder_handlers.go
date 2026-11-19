package server

import (
	"context"
	"errors"
	"os"

	"github.com/cam/mcp-onlyoffice/internal/builder"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Single-tool decision: builder_convert and builder_render are one tool
// ("builder_render") with a mode param ("convert" default, "render"
// optional, which additionally requires format). One tool keeps the surface
// small; mode selects Builder.Convert vs Builder.Render. Absence of the
// backend never degrades the core: the tool just reports unavailable.

// SetBuilder wires the optional Document Builder backend (nil-safe; a nil or
// unavailable builder only makes builder_render report unavailable).
func (h *ToolHandlers) SetBuilder(b *builder.Builder) {
	h.builder = b
}

// builderUnavailable is the user-facing text when the backend is absent. It
// states the argv contract the shim binary must accept.
const builderUnavailable = "document builder unavailable: set builder_path (MCP_ONLYOFFICE_BUILDER_PATH) to a shim binary accepting `<input> <output>` (convert) or `<input> <format> <output>` (render); all other tools keep working"

func (h *ToolHandlers) requireBuilder() (*builder.Builder, *mcp.CallToolResult) {
	if h.builder == nil || !h.builder.Available() {
		return nil, textError(builderUnavailable)
	}
	return h.builder, nil
}

// handleBuilderRender converts (mode=convert, default) or renders
// (mode=render, requires format) input into output. Risk-wise read/write on
// files: input and output are validated inside the allowed roots here and
// re-validated inside the builder package.
func (h *ToolHandlers) handleBuilderRender(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	b, errRes := h.requireBuilder()
	if errRes != nil {
		return errRes, nil
	}
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "convert"
	}
	input, _ := args["input"].(string)
	output, _ := args["output"].(string)
	format, _ := args["format"].(string)
	if input == "" || output == "" {
		return textError("input and output are required"), nil
	}
	resolvedIn, errRes := h.resolveRead(input)
	if errRes != nil {
		return errRes, nil
	}
	// prepareWrite refuses an existing output unless overwrite:true and
	// writes a .bak- backup when overwriting, like every other file writer.
	resolvedOut, errRes := h.prepareWrite(output, overwriteOK(args))
	if errRes != nil {
		return errRes, nil
	}
	var runErr error
	switch mode {
	case "convert":
		runErr = b.Convert(ctx, resolvedIn, resolvedOut)
	case "render":
		if format == "" {
			return textError("format is required in render mode"), nil
		}
		runErr = b.Render(ctx, resolvedIn, format, resolvedOut)
	default:
		return textError("mode must be convert or render"), nil
	}
	if runErr != nil {
		if errors.Is(runErr, builder.ErrUnavailable) {
			return textError(builderUnavailable), nil
		}
		return textError("builder_render: " + runErr.Error()), nil
	}
	version, err := b.Version(ctx)
	if err != nil {
		return textError("builder_render: " + err.Error()), nil
	}
	fi, err := os.Lstat(resolvedOut)
	if err != nil {
		return textError("builder_render: output vanished: " + err.Error()), nil
	}
	return jsonResult(map[string]any{"output": output, "bytes": fi.Size(), "builder_version": version}), nil
}

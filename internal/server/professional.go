package server

import (
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProfessionalToolNames es el perfil reducido de 16 herramientas.
var ProfessionalToolNames = []string{
	"create_word_document",
	"create_spreadsheet",
	"create_presentation",
	"write_docx",
	"write_spreadsheet",
	"write_presentation",
	"create_report",
	"list_documents",
	"get_document",
	"search_documents",
	"analyze_document",
	"validate_document",
	"replace_text",
	"insert_text",
	"get_selected_text",
	"open_document",
}

// IsProfessionalTool indica si el nombre pertenece al perfil profesional.
func IsProfessionalTool(name string) bool {
	for _, n := range ProfessionalToolNames {
		if n == name {
			return true
		}
	}
	return false
}

// FilterProfessionalTools conserva solo las herramientas del perfil,
// en el orden original de las definiciones.
func FilterProfessionalTools(defs []toolDef) []toolDef {
	out := make([]toolDef, 0, len(ProfessionalToolNames))
	for _, d := range defs {
		if IsProfessionalTool(d.Name) {
			out = append(out, d)
		}
	}
	return out
}

// HumanizeToolName convierte "create_word_document" en "Create Word Document".
func HumanizeToolName(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

func hasPrefixIn(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

var readOnlyPrefixes = []string{
	"read_", "list_", "get_", "search_", "find_",
	"head_", "tail_", "tree_", "disk_",
	"file_exists", "word_count", "storage_info",
	"analyze_", "validate_", "checksum_",
	"list_capabilities", "discover", "list_tools",
	"operational_memory",
}

// IsReadOnlyTool indica si la herramienta no modifica su entorno.
func IsReadOnlyTool(name string) bool {
	return hasPrefixIn(name, readOnlyPrefixes)
}

// IsDestructiveTool indica si la herramienta puede borrar datos.
func IsDestructiveTool(name string) bool {
	return hasPrefixIn(name, []string{"delete_", "extract_zip"})
}

var idempotentPrefixes = []string{"copy", "export", "render", "validate", "analyze"}

// IsIdempotentTool indica si repetir la llamada no tiene efecto adicional.
func IsIdempotentTool(name string) bool {
	return IsReadOnlyTool(name) || hasPrefixIn(name, idempotentPrefixes)
}

var openWorldPrefixes = []string{
	"network", "download", "self_improve", "planner",
	"python_engine", "document_agent", "run_workflow",
}

// IsOpenWorldTool indica si la herramienta interactúa con entidades externas.
func IsOpenWorldTool(name string) bool {
	return hasPrefixIn(name, openWorldPrefixes)
}

func boolPtr(b bool) *bool { return &b }

// ToolAnnotationsFor deriva las anotaciones MCP del nombre de la herramienta.
func ToolAnnotationsFor(name string) *mcp.ToolAnnotations {
	destructive := IsDestructiveTool(name)
	openWorld := IsOpenWorldTool(name)
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    IsReadOnlyTool(name),
		DestructiveHint: boolPtr(destructive),
		IdempotentHint:  IsIdempotentTool(name),
		OpenWorldHint:   boolPtr(openWorld),
		Title:           HumanizeToolName(name),
	}
}

// MCPToolFromDef convierte una definición interna en una herramienta MCP anotada.
func MCPToolFromDef(t toolDef) *mcp.Tool {
	return &mcp.Tool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
		Title:       HumanizeToolName(t.Name),
		Annotations: ToolAnnotationsFor(t.Name),
	}
}

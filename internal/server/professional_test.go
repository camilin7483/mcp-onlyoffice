package server

import (
	"strings"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/config"
)

func TestToolsetDefaultIsLegacy(t *testing.T) {
	cfg := config.Default()
	if cfg.Toolset != config.ToolsetLegacy {
		t.Fatalf("default toolset = %q, want legacy", cfg.Toolset)
	}
	if got := cfg.EffectiveToolset(); got != config.ToolsetLegacy {
		t.Fatalf("effective toolset = %q, want legacy", got)
	}
}

func TestToolsetEnvValues(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want string
	}{
		{"legacy", config.ToolsetLegacy},
		{"professional", config.ToolsetProfessional},
		{"  Professional  ", config.ToolsetProfessional},
		{"bogus", config.ToolsetLegacy},
		{"", config.ToolsetLegacy},
	} {
		t.Run("env="+tc.env, func(t *testing.T) {
			if tc.env == "" {
				t.Setenv(config.ToolsetEnvVar, "")
				cfg := config.Default()
				cfg.Toolset = ""
				if got := cfg.EffectiveToolset(); got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
				return
			}
			t.Setenv(config.ToolsetEnvVar, tc.env)
			cfg := config.Default()
			cfg.ApplyEnv()
			if got := cfg.EffectiveToolset(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProfessionalSelectionIsExact(t *testing.T) {
	h := &ToolHandlers{}
	all := h.Definitions()
	if len(all) != 74 {
		t.Fatalf("legacy definitions = %d, want 74", len(all))
	}

	known := make(map[string]bool, len(all))
	for _, d := range all {
		known[d.Name] = true
	}
	if len(ProfessionalToolNames) != 20 {
		t.Fatalf("professional names = %d, want 20", len(ProfessionalToolNames))
	}
	seen := make(map[string]bool, 16)
	for _, n := range ProfessionalToolNames {
		if seen[n] {
			t.Fatalf("duplicate professional tool %q", n)
		}
		seen[n] = true
		if !known[n] {
			t.Fatalf("professional tool %q missing from definitions", n)
		}
	}

	got := FilterProfessionalTools(all)
	if len(got) != 20 {
		t.Fatalf("filtered professional tools = %d, want 20", len(got))
	}
	for _, d := range got {
		if !seen[d.Name] {
			t.Fatalf("unexpected tool %q in professional set", d.Name)
		}
	}
}

func TestAllLegacyDefinitionsHaveAnnotations(t *testing.T) {
	for _, d := range (&ToolHandlers{}).Definitions() {
		tool := MCPToolFromDef(d)
		if tool.Title == "" {
			t.Errorf("tool %q has empty title", d.Name)
		}
		if tool.Annotations == nil {
			t.Errorf("tool %q has nil annotations", d.Name)
			continue
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Errorf("tool %q has nil destructiveHint", d.Name)
		}
		if tool.Annotations.OpenWorldHint == nil {
			t.Errorf("tool %q has nil openWorldHint", d.Name)
		}
		if tool.Annotations.Title == "" {
			t.Errorf("tool %q has empty annotations title", d.Name)
		}
	}
}

func TestAnnotationSpotChecks(t *testing.T) {
	for _, name := range []string{
		"list_documents", "get_document", "search_documents",
		"analyze_document", "validate_document", "get_selected_text",
	} {
		if !IsReadOnlyTool(name) {
			t.Errorf("%q should be read-only", name)
		}
		if !IsIdempotentTool(name) {
			t.Errorf("%q should be idempotent", name)
		}
	}
	for _, name := range []string{"create_word_document", "write_docx", "replace_text", "insert_text"} {
		if IsReadOnlyTool(name) {
			t.Errorf("%q should not be read-only", name)
		}
		if IsIdempotentTool(name) {
			t.Errorf("%q should not be idempotent", name)
		}
	}
	for _, name := range []string{"delete_document", "delete_file", "extract_zip"} {
		if !IsDestructiveTool(name) {
			t.Errorf("%q should be destructive", name)
		}
	}
	for _, name := range []string{"create_word_document", "list_documents", "get_document"} {
		if IsDestructiveTool(name) {
			t.Errorf("%q should not be destructive", name)
		}
	}
	for _, name := range []string{"copy_file", "export_document"} {
		if !IsIdempotentTool(name) {
			t.Errorf("%q should be idempotent", name)
		}
	}
	for _, name := range []string{
		"document_agent", "python_engine", "planner", "run_workflow", "self_improve",
	} {
		if !IsOpenWorldTool(name) {
			t.Errorf("%q should be open-world", name)
		}
	}
	for _, name := range []string{"list_documents", "create_word_document", "replace_text"} {
		if IsOpenWorldTool(name) {
			t.Errorf("%q should not be open-world", name)
		}
	}
}

func TestProfessionalHasNoFilesystemAdminTools(t *testing.T) {
	// Subcadenas típicas de herramientas de sistema de archivos o administración.
	blocked := []string{
		"_file", "_directory", "zip", "csv", "json", "markdown",
		"planner", "python_engine", "self_improve", "operational_memory",
		"discover", "workflow", "template", "reference", "touch_",
		"checksum", "disk_", "tree_", "head_", "tail_", "word_count",
		"copy_", "move_", "delete_", "find_", "import_", "export_",
		"create_document", "write_document", "rename_", "storage_info",
		"get_all_content", "create_table", "create_list", "load_tool",
	}
	for _, n := range ProfessionalToolNames {
		for _, b := range blocked {
			if strings.Contains(n, b) {
				t.Errorf("professional tool %q looks like a filesystem/admin tool (%q)", n, b)
			}
		}
	}
}

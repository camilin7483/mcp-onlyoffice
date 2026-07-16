package planner

import "strings"

type IntentType int

const (
	IntentUnknown IntentType = iota
	IntentCreateWorkbook
	IntentCreateChart
	IntentCreateDocument
	IntentCreatePresentation
	IntentAnalyzeData
	IntentCreateDashboard
	IntentCreatePDF
	IntentProcessImage
	IntentConvertFormat
	IntentGenerateReport
	IntentBatchProcess
)

type Intent struct {
	Primary   IntentType
	Secondary []IntentType
	Entities  map[string]string
	Outputs   []string
	Confidence float64
}

var intentSignals = map[IntentType][]string{
	IntentCreateWorkbook:    {"spreadsheet", "workbook", "xlsx", "excel", "sheet", "table data", "csv to excel", "formula", "pivot"},
	IntentCreateChart:       {"chart", "graph", "plot", "bar", "line chart", "pie", "histogram", "heatmap", "visualize", "visualization"},
	IntentCreateDocument:    {"document", "docx", "report", "letter", "memo", "word", "write", "create doc", "text file"},
	IntentCreatePresentation: {"presentation", "pptx", "slide", "powerpoint", "deck", "slideshow"},
	IntentAnalyzeData:       {"analyze", "analysis", "statistics", "kpi", "predict", "forecast", "aggregate", "summarize", "calculate"},
	IntentCreateDashboard:   {"dashboard", "kpi sheet", "summary dashboard", "executive dashboard", "metrics board", "kpi dashboard", "dash"},
	IntentCreatePDF:         {"pdf", "export pdf", "to pdf", "pdf document"},
	IntentProcessImage:      {"image", "picture", "photo", "screenshot", "convert image", "resize", "crop"},
	IntentConvertFormat:     {"convert", "transform", "change format", "export as", "save as"},
	IntentGenerateReport:    {"generate report", "monthly report", "annual report", "business report", "status report", "project report"},
	IntentBatchProcess:      {"batch", "multiple", "bulk", "all files", "process all", "convert all"},
}

var intentPriority = map[IntentType]int{
	IntentCreateWorkbook:    10,
	IntentCreateChart:       10,
	IntentCreatePresentation: 10,
	IntentAnalyzeData:       10,
	IntentCreateDashboard:   20,
	IntentCreatePDF:         30,
	IntentProcessImage:      35,
	IntentConvertFormat:     30,
	IntentGenerateReport:    20,
	IntentBatchProcess:      20,
	IntentCreateDocument:    5,
}

func AnalyzeIntent(objective string) Intent {
	obj := strings.ToLower(objective)
	entities := extractEntities(obj)
	outputs := extractOutputFormats(obj)

	var primary IntentType
	var secondary []IntentType
	var maxScore int
	var bestPriority int

	for it, signals := range intentSignals {
		score := countMatches(obj, signals)
		if score == 0 {
			continue
		}
		priority := intentPriority[it]
		if score > maxScore || (score == maxScore && priority > bestPriority) {
			if primary != IntentUnknown {
				secondary = append(secondary, primary)
			}
			maxScore = score
			primary = it
			bestPriority = priority
		} else {
			secondary = append(secondary, it)
		}
	}

	if primary == IntentUnknown && len(secondary) > 0 {
		primary = secondary[0]
		secondary = secondary[1:]
	}

	confidence := float64(maxScore) / 3.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return Intent{
		Primary:    primary,
		Secondary:  secondary,
		Entities:   entities,
		Outputs:    outputs,
		Confidence: confidence,
	}
}

func extractEntities(obj string) map[string]string {
	entities := make(map[string]string)
	words := strings.Fields(obj)
	for i, w := range words {
		lower := strings.ToLower(w)
		if lower == "called" || lower == "named" || lower == "as" {
			if i+1 < len(words) {
				entities["name"] = cleanEntity(words[i+1])
			}
		}
		if lower == "with" {
			if i+1 < len(words) {
				entities["param"] = cleanEntity(words[i+1])
			}
		}
	}
	return entities
}

func extractOutputFormats(obj string) []string {
	var formats []string
	formatKeywords := map[string]string{
		"docx": "docx", "word": "docx", "document": "docx",
		"xlsx": "xlsx", "excel": "xlsx", "spreadsheet": "xlsx",
		"pptx": "pptx", "powerpoint": "pptx", "presentation": "pptx",
		"pdf": "pdf", "csv": "csv", "json": "json",
		"png": "png", "jpg": "jpg", "jpeg": "jpg",
	}
	for word := range formatKeywords {
		if strings.Contains(obj, word) {
			formats = append(formats, formatKeywords[word])
		}
	}
	return formats
}

func countMatches(obj string, signals []string) int {
	words := strings.Fields(obj)
	wordSet := make(map[string]bool, len(words))
	for _, w := range words {
		cleaned := strings.Trim(w, "\"'.,;:!?()[]{}")
		wordSet[strings.ToLower(cleaned)] = true
	}

	count := 0
	for _, signal := range signals {
		signalWords := strings.Fields(signal)
		if len(signalWords) == 1 {
			if wordSet[signalWords[0]] {
				count++
			}
		} else {
			multiWord := strings.ToLower(signal)
			if strings.Contains(obj, multiWord) {
				count += 2
			}
		}
	}
	return count
}

func cleanEntity(s string) string {
	s = strings.Trim(s, "\"'.,;:!?")
	return s
}

var capabilityByIntent = map[IntentType][]string{
	IntentCreateWorkbook:    {"excel.create_workbook", "excel.insert_data"},
	IntentCreateChart:       {"charts.bar_chart", "charts.line_chart", "charts.pie_chart", "charts.heatmap"},
	IntentCreateDocument:    {"documents.create_report", "documents.create_document"},
	IntentCreatePresentation: {"presentations.create_presentation"},
	IntentAnalyzeData:       {"analytics.statistics", "analytics.aggregate", "analytics.frequency"},
	IntentCreateDashboard:   {"excel.kpi_sheet"},
	IntentCreatePDF:         {"pdf.create_pdf"},
	IntentProcessImage:      {"images.convert", "images.resize"},
	IntentGenerateReport:    {"documents.create_report", "excel.insert_data", "charts.bar_chart"},
	IntentBatchProcess:      {"excel.create_workbook", "documents.create_report", "presentations.create_presentation"},
}

func SelectCapabilities(intent Intent, available map[string]string) []string {
	candidates, ok := capabilityByIntent[intent.Primary]
	if !ok {
		return nil
	}

	var selected []string
	for _, cap := range candidates {
		if _, exists := available[cap]; exists {
			selected = append(selected, cap)
		}
	}
	return selected
}

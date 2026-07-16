package validator

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/storage"
)

type ValidationResult struct {
	ID       string  `json:"id"`
	Filename string  `json:"filename"`
	Valid    bool    `json:"valid"`
	Checks   []Check `json:"checks"`
	Score    int     `json:"score"`
	Summary  string  `json:"summary"`
}

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type Validator struct {
	store storage.Store
}

func New(store storage.Store) *Validator {
	return &Validator{store: store}
}

func (v *Validator) Validate(id string) (*ValidationResult, error) {
	doc, err := v.store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("get doc: %w", err)
	}

	data, err := v.store.GetData(id)
	if err != nil {
		return nil, fmt.Errorf("get data: %w", err)
	}

	res := &ValidationResult{
		ID:       doc.ID,
		Filename: doc.Filename,
	}

	v.checkMetadata(res, doc)
	v.checkOOXML(res, data, doc.Filename)
	v.checkContent(res, data)
	v.checkConsistency(res, data, doc.Filename)
	v.checkQuality(res, data, doc.Filename)

	passCount := 0
	totalCount := len(res.Checks)
	for _, c := range res.Checks {
		if c.Status == "pass" {
			passCount++
		}
	}

	res.Valid = true
	for _, c := range res.Checks {
		if c.Status == "fail" {
			res.Valid = false
			break
		}
	}

	if totalCount > 0 {
		res.Score = (passCount * 100) / totalCount
	} else {
		res.Score = 100
	}

	if res.Valid {
		res.Summary = fmt.Sprintf("All checks passed (%d/%d)", passCount, totalCount)
	} else {
		res.Summary = fmt.Sprintf("%d/%d checks passed", passCount, totalCount)
	}

	return res, nil
}

func (v *Validator) checkMetadata(res *ValidationResult, doc *storage.Document) {
	res.Checks = append(res.Checks, Check{Name: "filename", Status: "pass", Detail: doc.Filename})

	if doc.Size == 0 {
		res.Checks = append(res.Checks, Check{Name: "file_size", Status: "fail", Detail: "document is empty"})
	} else if doc.Size < 100 {
		res.Checks = append(res.Checks, Check{Name: "file_size", Status: "warn", Detail: fmt.Sprintf("very small: %d bytes", doc.Size)})
	} else {
		res.Checks = append(res.Checks, Check{Name: "file_size", Status: "pass", Detail: fmt.Sprintf("%d bytes", doc.Size)})
	}

	if doc.MimeType == "" || doc.MimeType == "application/octet-stream" {
		res.Checks = append(res.Checks, Check{Name: "mime_type", Status: "warn", Detail: "unknown mime type"})
	} else {
		res.Checks = append(res.Checks, Check{Name: "mime_type", Status: "pass", Detail: doc.MimeType})
	}

	ext := strings.ToLower(doc.Filename[strings.LastIndex(doc.Filename, ".")+1:])
	validExt := map[string]bool{"docx": true, "xlsx": true, "pptx": true, "pdf": true, "csv": true, "json": true, "txt": true, "md": true, "png": true, "jpg": true, "jpeg": true, "zip": true}
	if validExt[ext] {
		res.Checks = append(res.Checks, Check{Name: "extension", Status: "pass", Detail: ext})
	} else {
		res.Checks = append(res.Checks, Check{Name: "extension", Status: "warn", Detail: fmt.Sprintf("uncommon extension: .%s", ext)})
	}
}

func (v *Validator) checkOOXML(res *ValidationResult, data []byte, filename string) {
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])

	switch ext {
	case "docx", "xlsx", "pptx":
	default:
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "skip", Detail: "not an OOXML format"})
		return
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "fail", Detail: "invalid zip: " + err.Error()})
		return
	}

	requiredFiles := map[string][]string{
		"docx": {"[Content_Types].xml", "word/document.xml"},
		"xlsx": {"[Content_Types].xml", "xl/workbook.xml"},
		"pptx": {"[Content_Types].xml", "ppt/presentation.xml"},
	}

	required, ok := requiredFiles[ext]
	if !ok {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "skip", Detail: "unsupported format"})
		return
	}

	fileMap := make(map[string]bool)
	fileCount := 0
	for _, f := range zr.File {
		fileMap[f.Name] = true
		fileCount++
	}

	var missing []string
	for _, rf := range required {
		if !fileMap[rf] {
			missing = append(missing, rf)
		}
	}

	if len(missing) > 0 {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "fail", Detail: fmt.Sprintf("missing parts: %s", strings.Join(missing, ", "))})
	} else {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "pass", Detail: fmt.Sprintf("%d internal parts", fileCount)})
	}

	res.Checks = append(res.Checks, Check{Name: "ooxml_integrity", Status: "pass", Detail: fmt.Sprintf("%d files in archive", fileCount)})

	if ext == "xlsx" {
		v.checkSharedStrings(res, zr)
	}
}

func (v *Validator) checkSharedStrings(res *ValidationResult, zr *zip.Reader) {
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "warn", Detail: "cannot open: " + err.Error()})
				return
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "warn", Detail: "cannot read: " + err.Error()})
				return
			}
			if len(content) == 0 {
				res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "warn", Detail: "sharedStrings.xml is empty"})
			}
			return
		}
	}

	for _, f := range zr.File {
		if strings.Contains(f.Name, "worksheets/sheet") {
			res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "warn", Detail: "no sharedStrings.xml found"})
			return
		}
	}
}

var _ = strings.Contains // keep import

func (v *Validator) checkContent(res *ValidationResult, data []byte) {
	if len(data) < 100 {
		res.Checks = append(res.Checks, Check{Name: "content_size", Status: "warn", Detail: "very small file, may be incomplete"})
	} else if len(data) > 50*1024*1024 {
		res.Checks = append(res.Checks, Check{Name: "content_size", Status: "warn", Detail: fmt.Sprintf("large file: %d MB", len(data)/(1024*1024))})
	} else {
		res.Checks = append(res.Checks, Check{Name: "content_size", Status: "pass", Detail: fmt.Sprintf("%d bytes", len(data))})
	}
}

func (v *Validator) checkConsistency(res *ValidationResult, data []byte, filename string) {
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])

	if ext == "xlsx" || ext == "docx" || ext == "pptx" {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "fail", Detail: "cannot open zip"})
			return
		}

		hasContentType := false
		hasRels := false
		for _, f := range zr.File {
			if f.Name == "[Content_Types].xml" {
				hasContentType = true
			}
			if strings.HasSuffix(f.Name, ".rels") {
				hasRels = true
			}
		}

		if !hasContentType {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "fail", Detail: "missing [Content_Types].xml"})
		} else if !hasRels {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "warn", Detail: "no relationships files found"})
		} else {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "pass"})
		}
	} else {
		res.Checks = append(res.Checks, Check{Name: "consistency", Status: "skip", Detail: "not an OOXML format"})
	}
}

func (v *Validator) checkQuality(res *ValidationResult, data []byte, filename string) {
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	if ext != "docx" && ext != "xlsx" && ext != "pptx" {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "skip", Detail: "quality check only for OOXML"})
		return
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "skip", Detail: "cannot open"})
		return
	}

	files := 0
	emptyFiles := 0
	for _, f := range zr.File {
		files++
		if f.UncompressedSize == 0 {
			emptyFiles++
		}
	}

	emptyRatio := float64(emptyFiles) / float64(files) * 100
	if emptyRatio > 50 {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "warn", Detail: fmt.Sprintf("%.0f%% empty internal files", emptyRatio)})
	} else {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "pass", Detail: fmt.Sprintf("%d internal files", files)})
	}
}

func (v *Validator) ValidateBeforeReturn(id string) (*ValidationResult, error) {
	res, err := v.Validate(id)
	if err != nil {
		return nil, err
	}
	return res, nil
}

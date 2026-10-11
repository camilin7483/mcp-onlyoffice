package validator

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
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
	if doc.Filename == "" {
		res.Checks = append(res.Checks, Check{Name: "filename", Status: "fail", Detail: "missing filename: rename the file with a valid extension (e.g. report.docx)"})
	} else {
		res.Checks = append(res.Checks, Check{Name: "filename", Status: "pass", Detail: doc.Filename})
	}

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

	ext := fileExt(doc.Filename)
	validExt := map[string]bool{"docx": true, "xlsx": true, "pptx": true, "pdf": true, "csv": true, "json": true, "txt": true, "md": true, "png": true, "jpg": true, "jpeg": true, "zip": true}
	if ext == "" {
		res.Checks = append(res.Checks, Check{Name: "extension", Status: "fail", Detail: "file has no extension: rename it with a valid extension (e.g. report.docx)"})
	} else if validExt[ext] {
		res.Checks = append(res.Checks, Check{Name: "extension", Status: "pass", Detail: ext})
	} else {
		res.Checks = append(res.Checks, Check{Name: "extension", Status: "warn", Detail: fmt.Sprintf("uncommon extension: .%s", ext)})
	}
}

func (v *Validator) checkOOXML(res *ValidationResult, data []byte, filename string) {
	ext := fileExt(filename)

	switch ext {
	case "docx", "xlsx", "pptx":
	default:
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "skip", Detail: "not an OOXML format"})
		return
	}

	if len(data) < 4 || string(data[:2]) != "PK" {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "fail", Detail: "not a ZIP archive (missing PK signature): file is corrupt or was not saved as " + ext + ", recreate it from the source"})
		return
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "fail", Detail: "corrupt ZIP archive (" + err.Error() + "): re-upload or recreate the file"})
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
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "fail", Detail: fmt.Sprintf("missing parts: %s — file is incomplete, recreate it from the source", strings.Join(missing, ", "))})
	} else {
		res.Checks = append(res.Checks, Check{Name: "ooxml_structure", Status: "pass", Detail: fmt.Sprintf("%d internal parts", fileCount)})
	}

	v.checkContentTypes(res, zr, ext)
	v.checkXMLWellFormed(res, zr)
	v.checkMetadataParts(res, fileMap)
	v.checkRelTargets(res, zr)

	if len(missing) > 0 {
		res.Checks = append(res.Checks, Check{Name: "ooxml_integrity", Status: "fail", Detail: fmt.Sprintf("incomplete package: missing %s", strings.Join(missing, ", "))})
	} else {
		res.Checks = append(res.Checks, Check{Name: "ooxml_integrity", Status: "pass", Detail: fmt.Sprintf("%d files in archive", fileCount)})
	}

	if ext == "xlsx" {
		v.checkSharedStrings(res, zr)
	}
}

// checkContentTypes verifies the main part override exists in [Content_Types].xml.
func (v *Validator) checkContentTypes(res *ValidationResult, zr *zip.Reader, ext string) {
	mainPart := map[string]string{
		"docx": "/word/document.xml",
		"xlsx": "/xl/workbook.xml",
		"pptx": "/ppt/presentation.xml",
	}[ext]

	content, err := readZipPart(zr, "[Content_Types].xml")
	if err != nil {
		res.Checks = append(res.Checks, Check{Name: "ooxml_content_types", Status: "fail", Detail: "cannot read [Content_Types].xml (" + err.Error() + "): package is corrupt, recreate the file"})
		return
	}
	if !strings.Contains(string(content), mainPart) {
		res.Checks = append(res.Checks, Check{Name: "ooxml_content_types", Status: "fail", Detail: "missing content-type override for " + mainPart + ": editors will prompt for repair, recreate the file"})
		return
	}
	res.Checks = append(res.Checks, Check{Name: "ooxml_content_types", Status: "pass", Detail: "main part declared: " + mainPart})
}

// checkXMLWellFormed parses every XML part so malformed markup is caught
// before the file reaches ONLYOFFICE.
func (v *Validator) checkXMLWellFormed(res *ValidationResult, zr *zip.Reader) {
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			res.Checks = append(res.Checks, Check{Name: "ooxml_xml", Status: "fail", Detail: fmt.Sprintf("cannot open %s (%s): archive is corrupt, recreate the file", f.Name, err.Error())})
			return
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			res.Checks = append(res.Checks, Check{Name: "ooxml_xml", Status: "fail", Detail: fmt.Sprintf("cannot read %s (%s): archive is corrupt, recreate the file", f.Name, err.Error())})
			return
		}
		if len(content) == 0 {
			res.Checks = append(res.Checks, Check{Name: "ooxml_xml", Status: "fail", Detail: fmt.Sprintf("empty part %s: file is incomplete, recreate it from the source", f.Name)})
			return
		}
		var doc xmlNode
		if err := xml.Unmarshal(content, &doc); err != nil {
			res.Checks = append(res.Checks, Check{Name: "ooxml_xml", Status: "fail", Detail: fmt.Sprintf("malformed XML in %s (%s): editors will prompt for repair, recreate the file", f.Name, err.Error())})
			return
		}
	}
	res.Checks = append(res.Checks, Check{Name: "ooxml_xml", Status: "pass", Detail: "all XML parts well-formed"})
}

// checkMetadataParts warns when docProps are absent (file still opens,
// but title/author are lost).
func (v *Validator) checkMetadataParts(res *ValidationResult, fileMap map[string]bool) {
	var missing []string
	for _, p := range []string{"docProps/core.xml", "docProps/app.xml"} {
		if !fileMap[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		res.Checks = append(res.Checks, Check{Name: "ooxml_metadata", Status: "warn", Detail: fmt.Sprintf("missing metadata: %s (file still opens, re-save to restore properties)", strings.Join(missing, ", "))})
		return
	}
	res.Checks = append(res.Checks, Check{Name: "ooxml_metadata", Status: "pass", Detail: "core and app properties present"})
}

// checkRelTargets verifies every relationship target resolves to a part
// inside the package (external targets excepted).
func (v *Validator) checkRelTargets(res *ValidationResult, zr *zip.Reader) {
	fileMap := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		fileMap[f.Name] = true
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		content, err := readZipPart(zr, f.Name)
		if err != nil {
			continue // already reported by ooxml_xml
		}
		for _, target := range relTargets(content) {
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") || target == "" {
				continue
			}
			resolved := resolveRelTarget(f.Name, target)
			if !fileMap[resolved] {
				res.Checks = append(res.Checks, Check{Name: "ooxml_relationships", Status: "fail", Detail: fmt.Sprintf("%s points to missing part %s: editors will prompt for repair, recreate the file", f.Name, resolved)})
				return
			}
		}
	}
	res.Checks = append(res.Checks, Check{Name: "ooxml_relationships", Status: "pass", Detail: "all relationship targets resolve"})
}

func (v *Validator) checkSharedStrings(res *ValidationResult, zr *zip.Reader) {
	content, err := readZipPart(zr, "xl/sharedStrings.xml")
	if err != nil {
		for _, f := range zr.File {
			if strings.Contains(f.Name, "worksheets/sheet") {
				res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "warn", Detail: "no sharedStrings.xml found (inline strings still open, re-save to normalize)"})
				return
			}
		}
		res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "skip", Detail: "no shared strings used"})
		return
	}
	if len(content) == 0 {
		res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "fail", Detail: "sharedStrings.xml is empty: file is incomplete, recreate it from the source"})
		return
	}
	var doc xmlNode
	if err := xml.Unmarshal(content, &doc); err != nil {
		res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "fail", Detail: "malformed sharedStrings.xml (" + err.Error() + "): editors will prompt for repair, recreate the file"})
		return
	}
	res.Checks = append(res.Checks, Check{Name: "shared_strings", Status: "pass", Detail: fmt.Sprintf("%d bytes of shared strings", len(content))})
}

func fileExt(filename string) string {
	idx := strings.LastIndex(filename, ".")
	if idx < 0 || idx == len(filename)-1 {
		return ""
	}
	return strings.ToLower(filename[idx+1:])
}

func readZipPart(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("part not found: %s", name)
}

// xmlNode captures any element so well-formedness can be verified
// without binding every OOXML schema.
type xmlNode struct {
	XMLName  xml.Name
	Children []xmlNode `xml:",any"`
	Text     string    `xml:",chardata"`
}

// relTargets extracts Target attributes from a .rels part.
func relTargets(content []byte) []string {
	var targets []string
	dec := xml.NewDecoder(bytes.NewReader(content))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "Target" {
					targets = append(targets, attr.Value)
				}
			}
		}
	}
	return targets
}

// resolveRelTarget resolves a relationship target relative to the .rels
// part location (e.g. word/_rels/document.xml.rels + styles.xml).
func resolveRelTarget(relsPath, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	base := relsPath
	// strip "/_rels/<file>.rels" to get the part directory
	if idx := strings.Index(base, "/_rels/"); idx >= 0 {
		base = base[:idx]
	} else if strings.HasPrefix(base, "_rels/") {
		base = ""
	}
	// walk up for each ../
	for strings.HasPrefix(target, "../") {
		target = strings.TrimPrefix(target, "../")
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[:idx]
		} else {
			base = ""
		}
	}
	if base == "" {
		return target
	}
	return base + "/" + target
}

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
	ext := fileExt(filename)

	if ext == "xlsx" || ext == "docx" || ext == "pptx" {
		if len(data) == 0 {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "fail", Detail: "file is empty (0 bytes): nothing was saved, recreate the file"})
			return
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "fail", Detail: "cannot open ZIP archive (" + err.Error() + "): file is corrupt, re-upload or recreate it"})
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
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "fail", Detail: "missing [Content_Types].xml: package is not a valid Office file, recreate it from the source"})
		} else if !hasRels {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "warn", Detail: "no relationship (.rels) files found: file may still open, re-save to restore links"})
		} else {
			res.Checks = append(res.Checks, Check{Name: "consistency", Status: "pass"})
		}
	} else {
		res.Checks = append(res.Checks, Check{Name: "consistency", Status: "skip", Detail: "not an OOXML format"})
	}
}

func (v *Validator) checkQuality(res *ValidationResult, data []byte, filename string) {
	ext := fileExt(filename)
	if ext != "docx" && ext != "xlsx" && ext != "pptx" {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "skip", Detail: "quality check only for OOXML"})
		return
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "skip", Detail: "cannot open archive (" + err.Error() + ")"})
		return
	}

	files := 0
	emptyFiles := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue // directories have size 0 by design, not empty content
		}
		files++
		if f.UncompressedSize == 0 {
			emptyFiles++
		}
	}

	if files == 0 {
		res.Checks = append(res.Checks, Check{Name: "quality", Status: "fail", Detail: "archive contains no files: file is corrupt, recreate it from the source"})
		return
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

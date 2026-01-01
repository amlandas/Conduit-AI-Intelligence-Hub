package kb

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// Extractor defines the interface for text extraction from documents.
type Extractor interface {
	// Extract extracts text content from the given file.
	Extract(path string) (string, error)
	// Supported returns true if this extractor can handle the given extension.
	Supported(ext string) bool
	// Available returns true if the required tools are available.
	Available() bool
	// Name returns the extractor name for logging.
	Name() string
}

// ExtractorRegistry manages document extractors.
type ExtractorRegistry struct {
	extractors []Extractor
	logger     zerolog.Logger
}

// NewExtractorRegistry creates a new extractor registry with all available extractors.
func NewExtractorRegistry() *ExtractorRegistry {
	return &ExtractorRegistry{
		extractors: []Extractor{
			NewTextExtractor(),
			NewPDFExtractor(),
			NewDOCXExtractor(),
			NewODTExtractor(),
			NewDOCExtractor(),
			NewRTFExtractor(),
		},
		logger: observability.Logger("kb.extractors"),
	}
}

// Extract extracts text from a file using the appropriate extractor.
func (r *ExtractorRegistry) Extract(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))

	for _, extractor := range r.extractors {
		if extractor.Supported(ext) {
			if !extractor.Available() {
				r.logger.Warn().
					Str("path", path).
					Str("extractor", extractor.Name()).
					Msg("extractor not available, skipping file")
				return "", fmt.Errorf("extractor %s not available for %s", extractor.Name(), ext)
			}
			return extractor.Extract(path)
		}
	}

	// Fallback: try to read as text
	return r.readAsText(path)
}

// readAsText reads a file as plain text.
func (r *ExtractorRegistry) readAsText(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// CheckTools returns a map of tool availability.
func (r *ExtractorRegistry) CheckTools() map[string]bool {
	tools := make(map[string]bool)
	for _, extractor := range r.extractors {
		tools[extractor.Name()] = extractor.Available()
	}
	return tools
}

// TextExtractor handles plain text files.
type TextExtractor struct{}

// NewTextExtractor creates a new text extractor.
func NewTextExtractor() *TextExtractor {
	return &TextExtractor{}
}

func (e *TextExtractor) Name() string { return "text" }

func (e *TextExtractor) Available() bool { return true }

func (e *TextExtractor) Supported(ext string) bool {
	textExts := map[string]bool{
		".txt": true, ".md": true, ".rst": true,
		".go": true, ".py": true, ".js": true, ".ts": true,
		".java": true, ".rs": true, ".rb": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".cs": true, ".swift": true, ".kt": true,
		".sh": true, ".bash": true, ".zsh": true, ".fish": true,
		".ps1": true, ".bat": true, ".cmd": true,
		".json": true, ".yaml": true, ".yml": true,
		".xml": true, ".jsonld": true, ".toml": true,
		".ini": true, ".cfg": true,
		".csv": true, ".tsv": true,
	}
	return textExts[ext]
}

func (e *TextExtractor) Extract(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// PDFExtractor handles PDF files using pdftotext.
type PDFExtractor struct {
	toolPath string
}

// NewPDFExtractor creates a new PDF extractor.
func NewPDFExtractor() *PDFExtractor {
	path, _ := exec.LookPath("pdftotext")
	return &PDFExtractor{toolPath: path}
}

func (e *PDFExtractor) Name() string { return "pdf (pdftotext)" }

func (e *PDFExtractor) Available() bool { return e.toolPath != "" }

func (e *PDFExtractor) Supported(ext string) bool { return ext == ".pdf" }

func (e *PDFExtractor) Extract(path string) (string, error) {
	if e.toolPath == "" {
		return "", fmt.Errorf("pdftotext not available")
	}

	// pdftotext <input.pdf> - (output to stdout)
	cmd := exec.Command(e.toolPath, "-layout", path, "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext failed: %s: %w", stderr.String(), err)
	}

	return stdout.String(), nil
}

// DOCXExtractor handles .docx files (Office Open XML).
type DOCXExtractor struct{}

// NewDOCXExtractor creates a new DOCX extractor.
func NewDOCXExtractor() *DOCXExtractor {
	return &DOCXExtractor{}
}

func (e *DOCXExtractor) Name() string { return "docx (native)" }

func (e *DOCXExtractor) Available() bool { return true } // Pure Go, always available

func (e *DOCXExtractor) Supported(ext string) bool { return ext == ".docx" }

func (e *DOCXExtractor) Extract(path string) (string, error) {
	// DOCX is a ZIP archive containing XML files
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer r.Close()

	var text strings.Builder

	// Look for word/document.xml
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open document.xml: %w", err)
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return "", fmt.Errorf("read document.xml: %w", err)
			}

			// Extract text from XML
			extracted := extractTextFromXML(content, "w:t")
			text.WriteString(extracted)
			break
		}
	}

	return text.String(), nil
}

// ODTExtractor handles .odt files (OpenDocument Text).
type ODTExtractor struct{}

// NewODTExtractor creates a new ODT extractor.
func NewODTExtractor() *ODTExtractor {
	return &ODTExtractor{}
}

func (e *ODTExtractor) Name() string { return "odt (native)" }

func (e *ODTExtractor) Available() bool { return true } // Pure Go, always available

func (e *ODTExtractor) Supported(ext string) bool { return ext == ".odt" }

func (e *ODTExtractor) Extract(path string) (string, error) {
	// ODT is a ZIP archive containing XML files
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open odt: %w", err)
	}
	defer r.Close()

	var text strings.Builder

	// Look for content.xml
	for _, f := range r.File {
		if f.Name == "content.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open content.xml: %w", err)
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return "", fmt.Errorf("read content.xml: %w", err)
			}

			// Extract text from XML (text:p elements)
			extracted := extractTextFromODT(content)
			text.WriteString(extracted)
			break
		}
	}

	return text.String(), nil
}

// DOCExtractor handles .doc files (legacy Word format).
type DOCExtractor struct {
	toolPath string
	toolName string
}

// NewDOCExtractor creates a new DOC extractor.
func NewDOCExtractor() *DOCExtractor {
	e := &DOCExtractor{}

	// Try different tools based on OS
	if runtime.GOOS == "darwin" {
		// macOS has textutil built-in
		if path, err := exec.LookPath("textutil"); err == nil {
			e.toolPath = path
			e.toolName = "textutil"
			return e
		}
	}

	// Try antiword (cross-platform)
	if path, err := exec.LookPath("antiword"); err == nil {
		e.toolPath = path
		e.toolName = "antiword"
		return e
	}

	// Try LibreOffice as fallback
	for _, name := range []string{"soffice", "libreoffice"} {
		if path, err := exec.LookPath(name); err == nil {
			e.toolPath = path
			e.toolName = "libreoffice"
			return e
		}
	}

	return e
}

func (e *DOCExtractor) Name() string {
	if e.toolName != "" {
		return "doc (" + e.toolName + ")"
	}
	return "doc (not available)"
}

func (e *DOCExtractor) Available() bool { return e.toolPath != "" }

func (e *DOCExtractor) Supported(ext string) bool { return ext == ".doc" }

func (e *DOCExtractor) Extract(path string) (string, error) {
	if e.toolPath == "" {
		return "", fmt.Errorf("no .doc extractor available")
	}

	var cmd *exec.Cmd
	var stdout, stderr bytes.Buffer

	switch e.toolName {
	case "textutil":
		// textutil -convert txt -stdout input.doc
		cmd = exec.Command(e.toolPath, "-convert", "txt", "-stdout", path)
	case "antiword":
		// antiword input.doc
		cmd = exec.Command(e.toolPath, path)
	case "libreoffice":
		// LibreOffice needs a temp directory
		tmpDir, err := os.MkdirTemp("", "conduit-doc-")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		// soffice --headless --convert-to txt --outdir /tmp input.doc
		cmd = exec.Command(e.toolPath, "--headless", "--convert-to", "txt", "--outdir", tmpDir, path)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("libreoffice convert failed: %s: %w", stderr.String(), err)
		}

		// Read the converted file
		base := strings.TrimSuffix(filepath.Base(path), ".doc")
		txtPath := filepath.Join(tmpDir, base+".txt")
		content, err := os.ReadFile(txtPath)
		if err != nil {
			return "", fmt.Errorf("read converted file: %w", err)
		}
		return string(content), nil
	default:
		return "", fmt.Errorf("unknown doc tool: %s", e.toolName)
	}

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %s: %w", e.toolName, stderr.String(), err)
	}

	return stdout.String(), nil
}

// RTFExtractor handles .rtf files (Rich Text Format).
type RTFExtractor struct {
	toolPath string
	toolName string
}

// NewRTFExtractor creates a new RTF extractor.
func NewRTFExtractor() *RTFExtractor {
	e := &RTFExtractor{}

	// Try different tools based on OS
	if runtime.GOOS == "darwin" {
		// macOS has textutil built-in
		if path, err := exec.LookPath("textutil"); err == nil {
			e.toolPath = path
			e.toolName = "textutil"
			return e
		}
	}

	// Try unrtf (cross-platform)
	if path, err := exec.LookPath("unrtf"); err == nil {
		e.toolPath = path
		e.toolName = "unrtf"
		return e
	}

	// Try LibreOffice as fallback
	for _, name := range []string{"soffice", "libreoffice"} {
		if path, err := exec.LookPath(name); err == nil {
			e.toolPath = path
			e.toolName = "libreoffice"
			return e
		}
	}

	return e
}

func (e *RTFExtractor) Name() string {
	if e.toolName != "" {
		return "rtf (" + e.toolName + ")"
	}
	return "rtf (not available)"
}

func (e *RTFExtractor) Available() bool { return e.toolPath != "" }

func (e *RTFExtractor) Supported(ext string) bool { return ext == ".rtf" }

func (e *RTFExtractor) Extract(path string) (string, error) {
	if e.toolPath == "" {
		return "", fmt.Errorf("no .rtf extractor available")
	}

	var cmd *exec.Cmd
	var stdout, stderr bytes.Buffer

	switch e.toolName {
	case "textutil":
		// textutil -convert txt -stdout input.rtf
		cmd = exec.Command(e.toolPath, "-convert", "txt", "-stdout", path)
	case "unrtf":
		// unrtf --text input.rtf
		cmd = exec.Command(e.toolPath, "--text", path)
	case "libreoffice":
		// LibreOffice needs a temp directory
		tmpDir, err := os.MkdirTemp("", "conduit-rtf-")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		cmd = exec.Command(e.toolPath, "--headless", "--convert-to", "txt", "--outdir", tmpDir, path)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("libreoffice convert failed: %s: %w", stderr.String(), err)
		}

		// Read the converted file
		base := strings.TrimSuffix(filepath.Base(path), ".rtf")
		txtPath := filepath.Join(tmpDir, base+".txt")
		content, err := os.ReadFile(txtPath)
		if err != nil {
			return "", fmt.Errorf("read converted file: %w", err)
		}
		return string(content), nil
	default:
		return "", fmt.Errorf("unknown rtf tool: %s", e.toolName)
	}

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %s: %w", e.toolName, stderr.String(), err)
	}

	// unrtf adds some header text, try to clean it
	text := stdout.String()
	if e.toolName == "unrtf" {
		text = cleanUnrtfOutput(text)
	}

	return text, nil
}

// Helper functions

// extractTextFromXML extracts text content from XML elements with the given tag.
func extractTextFromXML(data []byte, tagName string) string {
	var result strings.Builder
	decoder := xml.NewDecoder(bytes.NewReader(data))

	inTag := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := token.(type) {
		case xml.StartElement:
			// Check if this is the target element (handling namespaces)
			localName := t.Name.Local
			if strings.HasSuffix(tagName, localName) || localName == strings.TrimPrefix(tagName, "w:") {
				inTag = true
			}
			// Add paragraph breaks
			if localName == "p" || localName == "br" {
				result.WriteString("\n")
			}
		case xml.EndElement:
			localName := t.Name.Local
			if strings.HasSuffix(tagName, localName) || localName == strings.TrimPrefix(tagName, "w:") {
				inTag = false
			}
		case xml.CharData:
			if inTag {
				result.Write(t)
			}
		}
	}

	return result.String()
}

// extractTextFromODT extracts text from ODT content.xml.
func extractTextFromODT(data []byte) string {
	var result strings.Builder
	decoder := xml.NewDecoder(bytes.NewReader(data))

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := token.(type) {
		case xml.StartElement:
			// Add paragraph/line breaks
			if t.Name.Local == "p" || t.Name.Local == "line-break" {
				result.WriteString("\n")
			} else if t.Name.Local == "tab" {
				result.WriteString("\t")
			} else if t.Name.Local == "s" {
				// Space element with count attribute
				for _, attr := range t.Attr {
					if attr.Name.Local == "c" {
						// Add specified number of spaces
						result.WriteString(strings.Repeat(" ", 1))
					}
				}
				result.WriteString(" ")
			}
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" {
				result.WriteString(string(t))
			}
		}
	}

	return result.String()
}

// cleanUnrtfOutput removes unrtf header and cleans up the output.
func cleanUnrtfOutput(text string) string {
	// unrtf adds a header like:
	// ### Translation from RTF performed by UnRTF, version X.X.X
	// ### ...
	// Find the end of header section
	lines := strings.Split(text, "\n")
	var result []string
	pastHeader := false

	for _, line := range lines {
		if !pastHeader {
			if !strings.HasPrefix(line, "###") && !strings.HasPrefix(line, "-*-") {
				pastHeader = true
			}
		}
		if pastHeader {
			result = append(result, line)
		}
	}

	// Also strip HTML tags if present
	text = strings.Join(result, "\n")
	re := regexp.MustCompile(`<[^>]*>`)
	text = re.ReplaceAllString(text, "")

	return strings.TrimSpace(text)
}

// ToolStatus represents the availability status of document extraction tools.
type ToolStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
}

// GetToolStatus returns the status of all document extraction tools.
func GetToolStatus() []ToolStatus {
	var status []ToolStatus

	// PDF tool
	if path, err := exec.LookPath("pdftotext"); err == nil {
		status = append(status, ToolStatus{Name: "pdftotext", Available: true, Path: path})
	} else {
		status = append(status, ToolStatus{Name: "pdftotext", Available: false})
	}

	// DOC tool
	docTool := ""
	docPath := ""
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("textutil"); err == nil {
			docTool = "textutil"
			docPath = path
		}
	}
	if docTool == "" {
		if path, err := exec.LookPath("antiword"); err == nil {
			docTool = "antiword"
			docPath = path
		}
	}
	if docTool != "" {
		status = append(status, ToolStatus{Name: docTool + " (doc)", Available: true, Path: docPath})
	} else {
		status = append(status, ToolStatus{Name: "antiword (doc)", Available: false})
	}

	// RTF tool
	rtfTool := ""
	rtfPath := ""
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("textutil"); err == nil {
			rtfTool = "textutil"
			rtfPath = path
		}
	}
	if rtfTool == "" {
		if path, err := exec.LookPath("unrtf"); err == nil {
			rtfTool = "unrtf"
			rtfPath = path
		}
	}
	if rtfTool != "" {
		status = append(status, ToolStatus{Name: rtfTool + " (rtf)", Available: true, Path: rtfPath})
	} else {
		status = append(status, ToolStatus{Name: "unrtf (rtf)", Available: false})
	}

	// DOCX/ODT - always available (pure Go)
	status = append(status, ToolStatus{Name: "docx (native)", Available: true})
	status = append(status, ToolStatus{Name: "odt (native)", Available: true})

	return status
}

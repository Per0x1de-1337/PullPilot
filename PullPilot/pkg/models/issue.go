package models

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)






type Issue struct {
	Path        string   // File path
	Line        int      // Line number
	Column      int      // Column number (optional)
	Severity    Severity // Issue severity
	Title       string   // Short issue title
	Description string   // Detailed description
	Suggestion  string   // Suggested fix (optional)
	Source      string   // Source of the issue (e.g., "golangci-lint", "llm")
}

type AffectedVersion struct {
    Introduced string
    Fixed      string
}









type File struct {
	Path    string // File path
	Content string // File content
}

type ReviewComment struct {
	Path      string // File path
	Line      int    // Line number
	Body      string // Comment body
	CommitID  string // Commit ID
	Position  int    // Position in the diff
}

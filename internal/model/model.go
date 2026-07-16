package model

import (
	"io/fs"
	"time"
)

type BacklinkMode string

const (
	BacklinksRepair BacklinkMode = "repair"
	BacklinksCheck  BacklinkMode = "check"
	BacklinksOff    BacklinkMode = "off"
)

type UnsupportedMode string

const (
	UnsupportedError UnsupportedMode = "error"
	UnsupportedWarn  UnsupportedMode = "warn"
)

type FrontmatterTitleMode string

const (
	FrontmatterTitleExact FrontmatterTitleMode = "exact-match"
	FrontmatterTitleNever FrontmatterTitleMode = "never"
)

type Request struct {
	Root              string
	ConfigPath        string
	Source            string
	NewName           string
	Reason            string
	Actor             string
	BatchID           string
	DryRun            bool
	BacklinksOverride BacklinkMode
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type Patch struct {
	Path          string `json:"path"`
	Start         int    `json:"start"`
	End           int    `json:"end"`
	Before        []byte `json:"-"`
	After         []byte `json:"-"`
	Kind          string `json:"kind"`
	OldTarget     string `json:"old_target,omitempty"`
	NewTarget     string `json:"new_target,omitempty"`
	ReferenceEdit bool   `json:"-"`
}

type FileChange struct {
	Path       string      `json:"path"`
	Role       string      `json:"role"`
	BeforeHash string      `json:"before_hash"`
	AfterHash  string      `json:"after_hash"`
	Mode       fs.FileMode `json:"mode"`
	Patches    []Patch     `json:"patches"`
}

type Plan struct {
	Root             string       `json:"root"`
	Source           string       `json:"source"`
	Destination      string       `json:"destination"`
	SourceHash       string       `json:"source_hash"`
	SourceMode       fs.FileMode  `json:"source_mode"`
	CaseOnly         bool         `json:"case_only"`
	Backlinks        BacklinkMode `json:"backlinks"`
	UnsupportedMode  UnsupportedMode
	FrontmatterTitle FrontmatterTitleMode
	FileChanges      []FileChange `json:"file_changes"`
	LinksUpdated     int          `json:"links_updated"`
	Warnings         []Warning    `json:"warnings"`
}

type Result struct {
	OperationID  string    `json:"operation_id,omitempty"`
	Status       string    `json:"status"`
	Source       string    `json:"source"`
	Destination  string    `json:"destination"`
	FilesChanged int       `json:"files_changed"`
	LinksUpdated int       `json:"links_updated"`
	Warnings     []Warning `json:"warnings"`
	LogPath      string    `json:"log_path,omitempty"`
}

type AuditContext struct {
	OperationID   string
	Actor         string
	Reason        string
	BatchID       string
	ToolVersion   string
	ConfigVersion int
	StartedAt     time.Time
}

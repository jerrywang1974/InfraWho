package backup

// DocumentVersion is the portable JSON export schema version.
const DocumentVersion = 1

// Document is the POST /export response and POST /import request body.
type Document struct {
	Version        int           `json:"version"`
	ExportedAt     string        `json:"exported_at,omitempty"`
	IncludeSecrets bool          `json:"include_secrets"`
	Assets         []ExportAsset `json:"assets"`
}

// ExportAsset is one asset plus nested accounts/jobs/notes (no owner FKs).
type ExportAsset struct {
	Name          string          `json:"name"`
	Hostname      string          `json:"hostname"`
	AssetType     string          `json:"asset_type"`
	OSFamily      string          `json:"os_family"`
	OSDetail      string          `json:"os_detail"`
	Environment   string          `json:"environment"`
	Purpose       string          `json:"purpose"`
	PrimaryIP     string          `json:"primary_ip"`
	AdditionalIPs []string        `json:"additional_ips"`
	Location      string          `json:"location"`
	Hypervisor    string          `json:"hypervisor"`
	Status        string          `json:"status"`
	ConfigNotes   string          `json:"config_notes"`
	Tags          []string        `json:"tags"`
	Accounts      []ExportAccount `json:"accounts"`
	Jobs          []ExportJob     `json:"jobs"`
	Notes         []ExportNote    `json:"notes"`
}

// ExportAccount is account metadata; Secret is set only when include_secrets.
type ExportAccount struct {
	Username    string  `json:"username"`
	AuthType    string  `json:"auth_type"`
	Description string  `json:"description"`
	HasSecret   bool    `json:"has_secret"`
	Secret      *string `json:"secret,omitempty"`
}

// ExportJob is a scheduled-job documentation row.
type ExportJob struct {
	Name          string `json:"name"`
	SchedulerType string `json:"scheduler_type"`
	ScheduleExpr  string `json:"schedule_expr"`
	CommandOrPath string `json:"command_or_path"`
	Description   string `json:"description"`
	EnabledDoc    bool   `json:"enabled_doc"`
}

// ExportNote is an asset note.
type ExportNote struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type exportRequest struct {
	Format         string `json:"format"`
	IncludeSecrets bool   `json:"include_secrets"`
}

type importResult struct {
	ImportedAssets   int `json:"imported_assets"`
	ImportedAccounts int `json:"imported_accounts"`
	ImportedJobs     int `json:"imported_jobs"`
	ImportedNotes    int `json:"imported_notes"`
}

package backup

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLen          = 128
	maxHostnameLen      = 255
	maxOSDetailLen      = 128
	maxPurposeLen       = 8 * 1024
	maxPrimaryIPLen     = 64
	maxAdditionalIPs    = 32
	maxLocationLen      = 128
	maxHypervisorLen    = 128
	maxConfigNotesLen   = 32 * 1024
	maxTags             = 64
	maxTagNameLen       = 64
	maxUsernameLen      = 255
	maxDescriptionLen   = 8 * 1024
	maxSecretLen        = 64 * 1024
	maxJobNameLen       = 128
	maxScheduleExprLen  = 256
	maxCommandOrPathLen = 8 * 1024
	maxNoteTitleLen     = 256
	maxNoteBodyLen      = 64 * 1024
)

var (
	validAssetTypes     = map[string]bool{"physical": true, "vm": true, "other": true}
	validOSFamilies     = map[string]bool{"linux": true, "windows": true, "other": true}
	validEnvironments   = map[string]bool{"prod": true, "staging": true, "dev": true, "lab": true, "other": true}
	writableStatuses    = map[string]bool{"active": true, "unknown": true}
	validAuthTypes      = map[string]bool{"password": true, "ssh_private_key": true, "api_token": true, "other": true}
	validSchedulerTypes = map[string]bool{"cron": true, "systemd_timer": true, "windows_task": true, "other": true}
)

// validateDocument checks required fields and enums before any DB writes.
func validateDocument(doc *Document) error {
	if doc == nil {
		return fmt.Errorf("%w: nil document", ErrInvalidDocument)
	}
	if doc.Version != 0 && doc.Version != DocumentVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidDocument, doc.Version)
	}
	if doc.Assets == nil {
		doc.Assets = []ExportAsset{}
	}
	for i := range doc.Assets {
		if err := validateExportAsset(&doc.Assets[i], i); err != nil {
			return err
		}
	}
	return nil
}

func validateExportAsset(a *ExportAsset, idx int) error {
	prefix := fmt.Sprintf("assets[%d]", idx)
	a.Name = strings.TrimSpace(a.Name)
	a.Hostname = strings.TrimSpace(a.Hostname)
	if a.Name == "" {
		return fmt.Errorf("%w: %s.name is required", ErrInvalidDocument, prefix)
	}
	if utf8.RuneCountInString(a.Name) > maxNameLen {
		return fmt.Errorf("%w: %s.name exceeds %d characters", ErrInvalidDocument, prefix, maxNameLen)
	}
	if a.Hostname == "" {
		return fmt.Errorf("%w: %s.hostname is required", ErrInvalidDocument, prefix)
	}
	if utf8.RuneCountInString(a.Hostname) > maxHostnameLen {
		return fmt.Errorf("%w: %s.hostname exceeds %d characters", ErrInvalidDocument, prefix, maxHostnameLen)
	}
	if !validAssetTypes[a.AssetType] {
		return fmt.Errorf("%w: %s.asset_type must be physical, vm, or other", ErrInvalidDocument, prefix)
	}
	if !validOSFamilies[a.OSFamily] {
		return fmt.Errorf("%w: %s.os_family must be linux, windows, or other", ErrInvalidDocument, prefix)
	}
	if !validEnvironments[a.Environment] {
		return fmt.Errorf("%w: %s.environment must be prod, staging, dev, lab, or other", ErrInvalidDocument, prefix)
	}
	status := strings.TrimSpace(a.Status)
	if status == "" || status == "retired" {
		status = "active"
	}
	if !writableStatuses[status] {
		return fmt.Errorf("%w: %s.status must be active or unknown", ErrInvalidDocument, prefix)
	}
	a.Status = status
	if utf8.RuneCountInString(a.OSDetail) > maxOSDetailLen {
		return fmt.Errorf("%w: %s.os_detail exceeds %d characters", ErrInvalidDocument, prefix, maxOSDetailLen)
	}
	if utf8.RuneCountInString(a.Purpose) > maxPurposeLen {
		return fmt.Errorf("%w: %s.purpose exceeds %d characters", ErrInvalidDocument, prefix, maxPurposeLen)
	}
	if utf8.RuneCountInString(a.PrimaryIP) > maxPrimaryIPLen {
		return fmt.Errorf("%w: %s.primary_ip exceeds %d characters", ErrInvalidDocument, prefix, maxPrimaryIPLen)
	}
	if a.AdditionalIPs == nil {
		a.AdditionalIPs = []string{}
	}
	if len(a.AdditionalIPs) > maxAdditionalIPs {
		return fmt.Errorf("%w: %s.additional_ips exceeds %d entries", ErrInvalidDocument, prefix, maxAdditionalIPs)
	}
	for _, ip := range a.AdditionalIPs {
		if utf8.RuneCountInString(ip) > maxPrimaryIPLen {
			return fmt.Errorf("%w: %s.additional_ips entry exceeds %d characters", ErrInvalidDocument, prefix, maxPrimaryIPLen)
		}
	}
	if utf8.RuneCountInString(a.Location) > maxLocationLen {
		return fmt.Errorf("%w: %s.location exceeds %d characters", ErrInvalidDocument, prefix, maxLocationLen)
	}
	if utf8.RuneCountInString(a.Hypervisor) > maxHypervisorLen {
		return fmt.Errorf("%w: %s.hypervisor exceeds %d characters", ErrInvalidDocument, prefix, maxHypervisorLen)
	}
	if utf8.RuneCountInString(a.ConfigNotes) > maxConfigNotesLen {
		return fmt.Errorf("%w: %s.config_notes exceeds %d characters", ErrInvalidDocument, prefix, maxConfigNotesLen)
	}
	tags, err := normalizeTags(a.Tags)
	if err != nil {
		return fmt.Errorf("%w: %s.tags: %v", ErrInvalidDocument, prefix, err)
	}
	a.Tags = tags
	if a.Accounts == nil {
		a.Accounts = []ExportAccount{}
	}
	for j := range a.Accounts {
		if err := validateExportAccount(&a.Accounts[j], prefix, j); err != nil {
			return err
		}
	}
	if a.Jobs == nil {
		a.Jobs = []ExportJob{}
	}
	for j := range a.Jobs {
		if err := validateExportJob(&a.Jobs[j], prefix, j); err != nil {
			return err
		}
	}
	if a.Notes == nil {
		a.Notes = []ExportNote{}
	}
	for j := range a.Notes {
		if err := validateExportNote(&a.Notes[j], prefix, j); err != nil {
			return err
		}
	}
	return nil
}

func validateExportAccount(acc *ExportAccount, assetPrefix string, idx int) error {
	prefix := fmt.Sprintf("%s.accounts[%d]", assetPrefix, idx)
	acc.Username = strings.TrimSpace(acc.Username)
	if acc.Username == "" {
		return fmt.Errorf("%w: %s.username is required", ErrInvalidDocument, prefix)
	}
	if utf8.RuneCountInString(acc.Username) > maxUsernameLen {
		return fmt.Errorf("%w: %s.username exceeds %d characters", ErrInvalidDocument, prefix, maxUsernameLen)
	}
	if !validAuthTypes[acc.AuthType] {
		return fmt.Errorf("%w: %s.auth_type must be password, ssh_private_key, api_token, or other", ErrInvalidDocument, prefix)
	}
	if utf8.RuneCountInString(acc.Description) > maxDescriptionLen {
		return fmt.Errorf("%w: %s.description exceeds %d characters", ErrInvalidDocument, prefix, maxDescriptionLen)
	}
	if acc.Secret != nil {
		if *acc.Secret == "" {
			return fmt.Errorf("%w: %s.secret is required when provided", ErrInvalidDocument, prefix)
		}
		if utf8.RuneCountInString(*acc.Secret) > maxSecretLen {
			return fmt.Errorf("%w: %s.secret exceeds %d characters", ErrInvalidDocument, prefix, maxSecretLen)
		}
	}
	return nil
}

func validateExportJob(j *ExportJob, assetPrefix string, idx int) error {
	prefix := fmt.Sprintf("%s.jobs[%d]", assetPrefix, idx)
	j.Name = strings.TrimSpace(j.Name)
	if j.Name == "" {
		return fmt.Errorf("%w: %s.name is required", ErrInvalidDocument, prefix)
	}
	if utf8.RuneCountInString(j.Name) > maxJobNameLen {
		return fmt.Errorf("%w: %s.name exceeds %d characters", ErrInvalidDocument, prefix, maxJobNameLen)
	}
	if !validSchedulerTypes[j.SchedulerType] {
		return fmt.Errorf("%w: %s.scheduler_type must be cron, systemd_timer, windows_task, or other", ErrInvalidDocument, prefix)
	}
	if utf8.RuneCountInString(j.ScheduleExpr) > maxScheduleExprLen {
		return fmt.Errorf("%w: %s.schedule_expr exceeds %d characters", ErrInvalidDocument, prefix, maxScheduleExprLen)
	}
	if utf8.RuneCountInString(j.CommandOrPath) > maxCommandOrPathLen {
		return fmt.Errorf("%w: %s.command_or_path exceeds %d characters", ErrInvalidDocument, prefix, maxCommandOrPathLen)
	}
	if utf8.RuneCountInString(j.Description) > maxDescriptionLen {
		return fmt.Errorf("%w: %s.description exceeds %d characters", ErrInvalidDocument, prefix, maxDescriptionLen)
	}
	return nil
}

func validateExportNote(n *ExportNote, assetPrefix string, idx int) error {
	prefix := fmt.Sprintf("%s.notes[%d]", assetPrefix, idx)
	if utf8.RuneCountInString(n.Title) > maxNoteTitleLen {
		return fmt.Errorf("%w: %s.title exceeds %d characters", ErrInvalidDocument, prefix, maxNoteTitleLen)
	}
	if utf8.RuneCountInString(n.Body) > maxNoteBodyLen {
		return fmt.Errorf("%w: %s.body exceeds %d characters", ErrInvalidDocument, prefix, maxNoteBodyLen)
	}
	return nil
}

func normalizeTags(tags []string) ([]string, error) {
	if tags == nil {
		return []string{}, nil
	}
	if len(tags) > maxTags {
		return nil, fmt.Errorf("at most %d tags allowed", maxTags)
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		name := strings.TrimSpace(t)
		if name == "" {
			return nil, fmt.Errorf("tag names must be non-empty")
		}
		if utf8.RuneCountInString(name) > maxTagNameLen {
			return nil, fmt.Errorf("tag name exceeds %d characters", maxTagNameLen)
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

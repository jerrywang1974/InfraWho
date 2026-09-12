package assets

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLen        = 128
	maxHostnameLen    = 255
	maxOSDetailLen    = 128
	maxPurposeLen     = 8 * 1024
	maxPrimaryIPLen   = 64
	maxAdditionalIPs  = 32
	maxLocationLen    = 128
	maxHypervisorLen  = 128
	maxConfigNotesLen = 32 * 1024
	maxTags           = 64
	maxTagNameLen     = 64
	defaultListLimit  = 50
	maxListLimit      = 200
)

var (
	validAssetTypes   = map[string]bool{"physical": true, "vm": true, "other": true}
	validOSFamilies   = map[string]bool{"linux": true, "windows": true, "other": true}
	validEnvironments = map[string]bool{"prod": true, "staging": true, "dev": true, "lab": true, "other": true}
	writableStatuses  = map[string]bool{"active": true, "unknown": true}
)

func clampLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
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

func validateCreate(in *createRequest) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(in.Name) > maxNameLen {
		return fmt.Errorf("name exceeds %d characters", maxNameLen)
	}
	if strings.TrimSpace(in.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	if utf8.RuneCountInString(in.Hostname) > maxHostnameLen {
		return fmt.Errorf("hostname exceeds %d characters", maxHostnameLen)
	}
	if !validAssetTypes[in.AssetType] {
		return fmt.Errorf("asset_type must be physical, vm, or other")
	}
	if !validOSFamilies[in.OSFamily] {
		return fmt.Errorf("os_family must be linux, windows, or other")
	}
	if !validEnvironments[in.Environment] {
		return fmt.Errorf("environment must be prod, staging, dev, lab, or other")
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if !writableStatuses[in.Status] {
		return fmt.Errorf("status must be active or unknown")
	}
	if utf8.RuneCountInString(in.OSDetail) > maxOSDetailLen {
		return fmt.Errorf("os_detail exceeds %d characters", maxOSDetailLen)
	}
	if utf8.RuneCountInString(in.Purpose) > maxPurposeLen {
		return fmt.Errorf("purpose exceeds %d characters", maxPurposeLen)
	}
	if utf8.RuneCountInString(in.PrimaryIP) > maxPrimaryIPLen {
		return fmt.Errorf("primary_ip exceeds %d characters", maxPrimaryIPLen)
	}
	if len(in.AdditionalIPs) > maxAdditionalIPs {
		return fmt.Errorf("additional_ips exceeds %d entries", maxAdditionalIPs)
	}
	for _, ip := range in.AdditionalIPs {
		if utf8.RuneCountInString(ip) > maxPrimaryIPLen {
			return fmt.Errorf("additional_ips entry exceeds %d characters", maxPrimaryIPLen)
		}
	}
	if utf8.RuneCountInString(in.Location) > maxLocationLen {
		return fmt.Errorf("location exceeds %d characters", maxLocationLen)
	}
	if utf8.RuneCountInString(in.Hypervisor) > maxHypervisorLen {
		return fmt.Errorf("hypervisor exceeds %d characters", maxHypervisorLen)
	}
	if utf8.RuneCountInString(in.ConfigNotes) > maxConfigNotesLen {
		return fmt.Errorf("config_notes exceeds %d characters", maxConfigNotesLen)
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return err
	}
	in.Tags = tags
	in.Name = strings.TrimSpace(in.Name)
	in.Hostname = strings.TrimSpace(in.Hostname)
	return nil
}

func validatePatch(in *patchRequest) error {
	if in.DeletedAt != nil {
		return fmt.Errorf("deleted_at cannot be set via PATCH")
	}
	if in.Status != nil {
		if *in.Status == "retired" {
			return fmt.Errorf("status=retired cannot be set via PATCH; use DELETE")
		}
		if !writableStatuses[*in.Status] {
			return fmt.Errorf("status must be active or unknown")
		}
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return fmt.Errorf("name is required")
		}
		if utf8.RuneCountInString(n) > maxNameLen {
			return fmt.Errorf("name exceeds %d characters", maxNameLen)
		}
		*in.Name = n
	}
	if in.Hostname != nil {
		h := strings.TrimSpace(*in.Hostname)
		if h == "" {
			return fmt.Errorf("hostname is required")
		}
		if utf8.RuneCountInString(h) > maxHostnameLen {
			return fmt.Errorf("hostname exceeds %d characters", maxHostnameLen)
		}
		*in.Hostname = h
	}
	if in.AssetType != nil && !validAssetTypes[*in.AssetType] {
		return fmt.Errorf("asset_type must be physical, vm, or other")
	}
	if in.OSFamily != nil && !validOSFamilies[*in.OSFamily] {
		return fmt.Errorf("os_family must be linux, windows, or other")
	}
	if in.Environment != nil && !validEnvironments[*in.Environment] {
		return fmt.Errorf("environment must be prod, staging, dev, lab, or other")
	}
	if in.OSDetail != nil && utf8.RuneCountInString(*in.OSDetail) > maxOSDetailLen {
		return fmt.Errorf("os_detail exceeds %d characters", maxOSDetailLen)
	}
	if in.Purpose != nil && utf8.RuneCountInString(*in.Purpose) > maxPurposeLen {
		return fmt.Errorf("purpose exceeds %d characters", maxPurposeLen)
	}
	if in.PrimaryIP != nil && utf8.RuneCountInString(*in.PrimaryIP) > maxPrimaryIPLen {
		return fmt.Errorf("primary_ip exceeds %d characters", maxPrimaryIPLen)
	}
	if in.AdditionalIPs != nil {
		if len(*in.AdditionalIPs) > maxAdditionalIPs {
			return fmt.Errorf("additional_ips exceeds %d entries", maxAdditionalIPs)
		}
		for _, ip := range *in.AdditionalIPs {
			if utf8.RuneCountInString(ip) > maxPrimaryIPLen {
				return fmt.Errorf("additional_ips entry exceeds %d characters", maxPrimaryIPLen)
			}
		}
	}
	if in.Location != nil && utf8.RuneCountInString(*in.Location) > maxLocationLen {
		return fmt.Errorf("location exceeds %d characters", maxLocationLen)
	}
	if in.Hypervisor != nil && utf8.RuneCountInString(*in.Hypervisor) > maxHypervisorLen {
		return fmt.Errorf("hypervisor exceeds %d characters", maxHypervisorLen)
	}
	if in.ConfigNotes != nil && utf8.RuneCountInString(*in.ConfigNotes) > maxConfigNotesLen {
		return fmt.Errorf("config_notes exceeds %d characters", maxConfigNotesLen)
	}
	if in.Tags != nil {
		tags, err := normalizeTags(*in.Tags)
		if err != nil {
			return err
		}
		*in.Tags = tags
	}
	return nil
}

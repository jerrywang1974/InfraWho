package jobs

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLen        = 128
	maxScheduleLen    = 1024
	maxCommandLen     = 8 * 1024
	maxDescriptionLen = 8 * 1024
	defaultListLimit  = 50
	maxListLimit      = 200
)

var validSchedulerTypes = map[string]bool{
	"cron": true, "systemd_timer": true, "windows_task": true, "other": true,
}

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

func validateCreate(in *createRequest) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return fmt.Errorf("name exceeds %d characters", maxNameLen)
	}
	in.Name = name
	if !validSchedulerTypes[in.SchedulerType] {
		return fmt.Errorf("scheduler_type must be cron, systemd_timer, windows_task, or other")
	}
	if utf8.RuneCountInString(in.ScheduleExpr) > maxScheduleLen {
		return fmt.Errorf("schedule_expr exceeds %d characters", maxScheduleLen)
	}
	if utf8.RuneCountInString(in.CommandOrPath) > maxCommandLen {
		return fmt.Errorf("command_or_path exceeds %d characters", maxCommandLen)
	}
	if utf8.RuneCountInString(in.Description) > maxDescriptionLen {
		return fmt.Errorf("description exceeds %d characters", maxDescriptionLen)
	}
	if in.EnabledDoc == nil {
		t := true
		in.EnabledDoc = &t
	}
	return nil
}

func validatePatch(in *patchRequest) error {
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
	if in.SchedulerType != nil && !validSchedulerTypes[*in.SchedulerType] {
		return fmt.Errorf("scheduler_type must be cron, systemd_timer, windows_task, or other")
	}
	if in.ScheduleExpr != nil && utf8.RuneCountInString(*in.ScheduleExpr) > maxScheduleLen {
		return fmt.Errorf("schedule_expr exceeds %d characters", maxScheduleLen)
	}
	if in.CommandOrPath != nil && utf8.RuneCountInString(*in.CommandOrPath) > maxCommandLen {
		return fmt.Errorf("command_or_path exceeds %d characters", maxCommandLen)
	}
	if in.Description != nil && utf8.RuneCountInString(*in.Description) > maxDescriptionLen {
		return fmt.Errorf("description exceeds %d characters", maxDescriptionLen)
	}
	return nil
}

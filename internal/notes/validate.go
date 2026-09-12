package notes

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxTitleLen      = 256
	maxBodyLen       = 64 * 1024
	defaultListLimit = 50
	maxListLimit     = 200
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

func validateCreate(in *createRequest) error {
	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) > maxTitleLen {
		return fmt.Errorf("title exceeds %d characters", maxTitleLen)
	}
	in.Title = title
	if utf8.RuneCountInString(in.Body) > maxBodyLen {
		return fmt.Errorf("body exceeds %d characters", maxBodyLen)
	}
	return nil
}

func validatePatch(in *patchRequest) error {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if utf8.RuneCountInString(t) > maxTitleLen {
			return fmt.Errorf("title exceeds %d characters", maxTitleLen)
		}
		*in.Title = t
	}
	if in.Body != nil && utf8.RuneCountInString(*in.Body) > maxBodyLen {
		return fmt.Errorf("body exceeds %d characters", maxBodyLen)
	}
	return nil
}

package accounts

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxUsernameLen    = 255
	maxDescriptionLen = 8 * 1024
	maxSecretLen      = 64 * 1024
)

var validAuthTypes = map[string]bool{
	"password":        true,
	"ssh_private_key": true,
	"api_token":       true,
	"other":           true,
}

func validateAuthType(v string) error {
	if !validAuthTypes[v] {
		return fmt.Errorf("auth_type must be password, ssh_private_key, api_token, or other")
	}
	return nil
}

func validateCreate(in *createRequest) error {
	if strings.TrimSpace(in.Username) == "" {
		return fmt.Errorf("username is required")
	}
	if utf8.RuneCountInString(in.Username) > maxUsernameLen {
		return fmt.Errorf("username exceeds %d characters", maxUsernameLen)
	}
	if in.AuthType == "" {
		return fmt.Errorf("auth_type is required")
	}
	if err := validateAuthType(in.AuthType); err != nil {
		return err
	}
	if utf8.RuneCountInString(in.Description) > maxDescriptionLen {
		return fmt.Errorf("description exceeds %d characters", maxDescriptionLen)
	}
	if in.Secret != nil {
		if *in.Secret == "" {
			return fmt.Errorf("secret is required when provided")
		}
		if utf8.RuneCountInString(*in.Secret) > maxSecretLen {
			return fmt.Errorf("secret exceeds %d characters", maxSecretLen)
		}
	}
	return nil
}

func validatePatch(in *patchRequest) error {
	if in.Username != nil {
		if strings.TrimSpace(*in.Username) == "" {
			return fmt.Errorf("username is required")
		}
		if utf8.RuneCountInString(*in.Username) > maxUsernameLen {
			return fmt.Errorf("username exceeds %d characters", maxUsernameLen)
		}
	}
	if in.AuthType != nil {
		if err := validateAuthType(*in.AuthType); err != nil {
			return err
		}
	}
	if in.Description != nil && utf8.RuneCountInString(*in.Description) > maxDescriptionLen {
		return fmt.Errorf("description exceeds %d characters", maxDescriptionLen)
	}
	return nil
}

func validateRotate(in *rotateRequest) error {
	if in.Secret == "" {
		return fmt.Errorf("secret is required")
	}
	if utf8.RuneCountInString(in.Secret) > maxSecretLen {
		return fmt.Errorf("secret exceeds %d characters", maxSecretLen)
	}
	if in.AuthType != nil {
		if err := validateAuthType(*in.AuthType); err != nil {
			return err
		}
	}
	return nil
}

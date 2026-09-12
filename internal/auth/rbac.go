package auth

// Role is a user RBAC role.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

// HasAtLeast reports whether r is at least as privileged as min.
// Ordering: viewer < operator < admin.
func (r Role) HasAtLeast(min Role) bool {
	return roleRank(r) >= roleRank(min)
}

func roleRank(r Role) int {
	switch r {
	case RoleViewer:
		return 1
	case RoleOperator:
		return 2
	case RoleAdmin:
		return 3
	default:
		return 0
	}
}

// CanReveal is true for operator and admin.
func (r Role) CanReveal() bool {
	return r == RoleOperator || r == RoleAdmin
}

// CanManageUsers is true only for admin.
func (r Role) CanManageUsers() bool {
	return r == RoleAdmin
}

// CanExportSecrets is true only for admin.
func (r Role) CanExportSecrets() bool {
	return r == RoleAdmin
}

// CanSoftDeleteOrPurge is true only for admin.
func (r Role) CanSoftDeleteOrPurge() bool {
	return r == RoleAdmin
}

package auth

import "context"

type ctxKey int

const (
	ctxUserKey ctxKey = iota + 1
	ctxSessionKey
)

func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxUserKey, u)
}

func withSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxSessionKey, s)
}

// UserFromContext returns the authenticated user, if any.
func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxUserKey).(*User)
	return u, ok && u != nil
}

// SessionFromContext returns the active session, if any.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(ctxSessionKey).(*Session)
	return s, ok && s != nil
}

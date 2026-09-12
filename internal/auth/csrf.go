package auth

import (
	"net/http"
	"net/url"
	"strings"
)

// CheckOrigin validates Origin (or Referer) against the request host and optional allow-list.
// Safe methods (GET, HEAD, OPTIONS) always pass.
// X-Forwarded-Host is honored only when trustProxy is true.
func CheckOrigin(r *http.Request, trustedOrigins []string, trustProxy bool) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
			if u, err := url.Parse(ref); err == nil && u.Host != "" {
				origin = u.Scheme + "://" + u.Host
			}
		}
	}
	if origin == "" {
		return false
	}
	ou, err := url.Parse(origin)
	if err != nil || ou.Scheme == "" || ou.Host == "" {
		return false
	}
	originKey := strings.ToLower(ou.Scheme + "://" + ou.Host)
	originHost := strings.ToLower(ou.Host)

	allowedHosts := map[string]struct{}{}
	allowedOrigins := map[string]struct{}{}
	for _, h := range requestHosts(r, trustProxy) {
		allowedHosts[strings.ToLower(h)] = struct{}{}
	}
	for _, o := range trustedOrigins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if u, err := url.Parse(o); err == nil && u.Scheme != "" && u.Host != "" {
			allowedOrigins[strings.ToLower(u.Scheme+"://"+u.Host)] = struct{}{}
			continue
		}
		// Bare host entry matches any scheme for that host.
		allowedHosts[strings.ToLower(o)] = struct{}{}
	}

	if _, ok := allowedOrigins[originKey]; ok {
		return true
	}
	_, ok := allowedHosts[originHost]
	return ok
}

func requestHosts(r *http.Request, trustProxy bool) []string {
	var hosts []string
	if trustProxy {
		if xfh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xfh != "" {
			if i := strings.IndexByte(xfh, ','); i >= 0 {
				xfh = xfh[:i]
			}
			hosts = append(hosts, strings.TrimSpace(xfh))
		}
	}
	if r.Host != "" {
		hosts = append(hosts, r.Host)
	}
	out := make([]string, 0, len(hosts))
	seen := map[string]struct{}{}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	return out
}

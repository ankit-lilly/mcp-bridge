package oauth

import (
	"net/http"
	"strings"
)

type ChallengeInfo struct {
	Scope               string
	ResourceMetadataURL string
	Error               string
	ErrorDescription    string
}

func ParseWWWAuthenticate(headers http.Header) *ChallengeInfo {
	info := &ChallengeInfo{}
	for _, h := range headers.Values("Www-Authenticate") {
		parseBearer(h, info)
	}
	if info.Scope == "" && info.ResourceMetadataURL == "" && info.Error == "" {
		return nil
	}
	return info
}

func parseBearer(header string, info *ChallengeInfo) {
	scheme, params, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return
	}
	for params != "" {
		params = strings.TrimLeft(params, " ,")
		if params == "" {
			break
		}
		key, rest, _ := strings.Cut(params, "=")
		key = strings.TrimSpace(key)
		if rest == "" {
			break
		}
		var value string
		if rest[0] == '"' {
			rest = rest[1:]
			value, rest, _ = strings.Cut(rest, "\"")
			rest, _, _ = strings.Cut(rest, ",")
		} else {
			value, rest, _ = strings.Cut(rest, ",")
			value = strings.TrimSpace(value)
		}
		params = rest

		switch strings.ToLower(key) {
		case "scope":
			info.Scope = value
		case "resource_metadata":
			info.ResourceMetadataURL = value
		case "error":
			info.Error = value
		case "error_description":
			info.ErrorDescription = value
		}
	}
}

package resume

import (
	"net/mail"
	"net/url"
	"strings"
)

// profileWebLink accepts web addresses, including common scheme-less profile
// links, without activating script/file URLs or embedded credentials in a PDF.
func profileWebLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\t ") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme == "" {
		if strings.HasPrefix(raw, "//") {
			raw = "https:" + raw
		} else {
			raw = "https://" + raw
		}
		u, err = url.Parse(raw)
		if err != nil {
			return ""
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return u.String()
}

func profileEmailLink(raw string) string {
	address, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return (&url.URL{Scheme: "mailto", Path: address.Address, OmitHost: true}).String()
}

func profileDOILink(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "doi:") {
		raw = strings.TrimSpace(raw[4:])
	}
	if strings.HasPrefix(raw, "10.") && strings.Contains(raw, "/") && !strings.ContainsAny(raw, "\r\n\t ") {
		return (&url.URL{Scheme: "https", Host: "doi.org", Path: "/" + raw}).String()
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") || strings.HasPrefix(raw, "doi.org/") {
		return profileWebLink(raw)
	}
	return ""
}

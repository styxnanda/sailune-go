package model

import (
	"errors"
	"strings"
)

func SiteHost(site Site) (string, error) {
	switch site {
	case AO3:
		return "archiveofourown.org", nil
	case FFN:
		return "www.fanfiction.net", nil
	default:
		return "", errors.New("site must be ao3 or ffn")
	}
}

func SiteDomain(site Site, domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	switch site {
	case AO3:
		return domain == "archiveofourown.org" || domain == "www.archiveofourown.org"
	case FFN:
		return domain == "fanfiction.net" || domain == "www.fanfiction.net" || domain == "m.fanfiction.net"
	}
	return false
}

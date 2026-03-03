package od

import (
	"fmt"
	"strings"
)

func Detect(client *Client, shareURL string) (ShareType, string, []byte, error) {
	finalURL, body, err := client.ResolveURL(shareURL)
	if err != nil {
		return 0, "", nil, fmt.Errorf("failed to follow share link: %w", err)
	}

	lower := strings.ToLower(finalURL)
	switch {
	case strings.Contains(lower, "sharepoint.com"):
		return TypeSP, finalURL, body, nil
	case strings.Contains(lower, "onedrive.live.com"),
		strings.Contains(lower, "1drv.ms"),
		strings.Contains(lower, "microsoftpersonalcontent.com"):
		return TypePersonal, finalURL, body, nil
	default:
		bodyStr := strings.ToLower(string(body))
		if strings.Contains(bodyStr, "sharepoint.com") {
			return TypeSP, finalURL, body, nil
		}
		if strings.Contains(bodyStr, "onedrive.live.com") || strings.Contains(bodyStr, "1drv.ms") {
			return TypePersonal, finalURL, body, nil
		}
		return 0, finalURL, body, fmt.Errorf("unrecognized share link type (final URL: %s)", finalURL)
	}
}

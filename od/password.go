package od

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type PwdHandler struct {
	Client *Client
}

func NeedPwd(finalURL string, body []byte) bool {
	lower := strings.ToLower(finalURL)
	if strings.Contains(lower, "guestaccess.aspx") {
		return true
	}

	s := string(body)
	if strings.Contains(s, "txtPassword") {
		return true
	}
	if strings.Contains(strings.ToLower(s), "type=\"password\"") {
		return true
	}
	return false
}

func (h *PwdHandler) Submit(finalURL string, body []byte, pwd string, origURL string) (string, []byte, error) {
	if h.Client.Verbose {
		fmt.Println("[Password] Detected password-protected share")
	}

	siteBase := siteBaseURL(finalURL)
	if h.Client.Verbose {
		fmt.Printf("[Password] Site base URL: %s\n", siteBase)
	}

	if strings.Contains(string(body), "__VIEWSTATE") || strings.Contains(string(body), "txtPassword") {
		if h.Client.Verbose {
			fmt.Println("[Password] Trying traditional form POST...")
		}
		newURL, newBody, err := h.submitForm(finalURL, body, pwd)
		if err == nil && !NeedPwd(newURL, newBody) {
			return newURL, newBody, nil
		}
		if h.Client.Verbose {
			fmt.Printf("[Password] Form POST didn't work: %v\n", err)
		}
	}

	if h.Client.Verbose {
		fmt.Println("[Password] Trying GetSharingLinkData API...")
	}
	err := h.submitAPI(siteBase, origURL, pwd)
	if err == nil {
		newURL, newBody, err := h.Client.ResolveURL(origURL)
		if err == nil && !NeedPwd(newURL, newBody) {
			return newURL, newBody, nil
		}
		if h.Client.Verbose {
			fmt.Printf("[Password] GetSharingLinkData succeeded but page still requires password\n")
		}
	} else if h.Client.Verbose {
		fmt.Printf("[Password] GetSharingLinkData failed: %v\n", err)
	}

	if h.Client.Verbose {
		fmt.Println("[Password] Trying guestaccess.aspx POST...")
	}
	err = h.submitGuest(finalURL, pwd)
	if err == nil {
		newURL, newBody, err := h.Client.ResolveURL(origURL)
		if err == nil && !NeedPwd(newURL, newBody) {
			return newURL, newBody, nil
		}
	}
	if h.Client.Verbose {
		fmt.Printf("[Password] guestaccess POST result: %v\n", err)
	}

	return "", nil, fmt.Errorf("all password submission approaches failed")
}

func (h *PwdHandler) submitForm(pageURL string, pageBody []byte, pwd string) (string, []byte, error) {
	bodyStr := string(pageBody)
	action := formAction(bodyStr, pageURL)
	formData := hiddenFields(bodyStr)

	pwdFields := []string{"txtPassword", "ctl00$PlaceHolderMain$ctl00$TxtPassword",
		"ctl00$PlaceHolderMain$passwordTextBox", "Password"}
	for _, f := range pwdFields {
		formData[f] = pwd
	}

	btn := btnName(bodyStr)
	if btn != "" {
		formData[btn] = "Verify"
	}

	resp, err := h.Client.HTTP.R().
		SetFormData(formData).
		Post(action)
	if err != nil {
		return "", nil, err
	}

	newURL := resp.RawResponse.Request.URL.String()
	respBody := resp.Body()

	respStr := string(respBody)
	if strings.Contains(respStr, "The password you supplied is not correct") ||
		strings.Contains(respStr, "密码不正确") {
		return "", nil, fmt.Errorf("incorrect password")
	}

	return newURL, respBody, nil
}

func (h *PwdHandler) submitAPI(siteBase, shareURL, pwd string) error {
	digest, err := h.getDigest(siteBase)
	if err != nil {
		if h.Client.Verbose {
			fmt.Printf("[Password] Failed to get FormDigest: %v\n", err)
		}
		digest = ""
	}

	apiURL := siteBase + "/_api/web/GetSharingLinkData"
	reqBody := fmt.Sprintf(`{"linkUrl":"%s","password":"%s"}`, shareURL, pwd)

	req := h.Client.HTTP.R().
		SetHeader("Content-Type", "application/json;odata=verbose").
		SetHeader("Accept", "application/json;odata=verbose").
		SetBody(reqBody)

	if digest != "" {
		req.SetHeader("X-RequestDigest", digest)
	}

	resp, err := req.Post(apiURL)
	if err != nil {
		return err
	}

	if resp.StatusCode() == 200 {
		return nil
	}

	return fmt.Errorf("GetSharingLinkData returned %d", resp.StatusCode())
}

func (h *PwdHandler) submitGuest(guestURL, pwd string) error {
	u, _ := url.Parse(guestURL)
	shareToken := u.Query().Get("share")

	formData := map[string]string{
		"txtPassword": pwd,
		"Password":    pwd,
	}
	if shareToken != "" {
		formData["share"] = shareToken
	}

	resp, err := h.Client.HTTP.R().
		SetFormData(formData).
		Post(guestURL)
	if err != nil {
		return err
	}

	if resp.StatusCode() == 200 || resp.StatusCode() == 302 {
		return nil
	}

	return fmt.Errorf("guestaccess POST returned %d", resp.StatusCode())
}

func (h *PwdHandler) getDigest(siteBase string) (string, error) {
	apiURL := siteBase + "/_api/contextinfo"

	resp, err := h.Client.HTTP.R().
		SetHeader("Content-Type", "application/json;odata=verbose").
		SetHeader("Accept", "application/json;odata=verbose").
		SetBody("{}").
		Post(apiURL)
	if err != nil {
		return "", err
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("contextinfo returned %d", resp.StatusCode())
	}

	var result struct {
		D struct {
			GetContextWebInformation struct {
				FormDigestValue string `json:"FormDigestValue"`
			} `json:"GetContextWebInformation"`
		} `json:"d"`
	}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		re := regexp.MustCompile(`"FormDigestValue":"([^"]+)"`)
		matches := re.FindSubmatch(resp.Body())
		if len(matches) >= 2 {
			return string(matches[1]), nil
		}
		return "", fmt.Errorf("failed to parse FormDigest: %w", err)
	}
	return result.D.GetContextWebInformation.FormDigestValue, nil
}

func siteBaseURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 {
		prefix := strings.ToLower(parts[0])
		if prefix == "sites" || prefix == "personal" || prefix == "teams" {
			return fmt.Sprintf("%s://%s/%s/%s", u.Scheme, u.Host, parts[0], parts[1])
		}
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

func hiddenFields(html string) map[string]string {
	fields := make(map[string]string)
	re := regexp.MustCompile(`<input[^>]+type="hidden"[^>]*>`)
	inputs := re.FindAllString(html, -1)
	for _, input := range inputs {
		name := getAttr(input, "name")
		val := getAttr(input, "value")
		if name != "" {
			fields[name] = unescHTML(val)
		}
	}
	return fields
}

func getAttr(tag, attr string) string {
	re := regexp.MustCompile(attr + `="([^"]*)"`)
	matches := re.FindStringSubmatch(tag)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func formAction(html, pageURL string) string {
	re := regexp.MustCompile(`<form[^>]+action="([^"]*)"`)
	matches := re.FindStringSubmatch(html)
	if len(matches) >= 2 {
		action := strings.ReplaceAll(matches[1], "&amp;", "&")
		if strings.HasPrefix(action, "/") {
			u, err := url.Parse(pageURL)
			if err == nil {
				return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, action)
			}
		}
		if strings.HasPrefix(action, "http") {
			return action
		}
	}
	return pageURL
}

func btnName(html string) string {
	re := regexp.MustCompile(`<input[^>]+type="submit"[^>]+name="([^"]*)"`)
	matches := re.FindStringSubmatch(html)
	if len(matches) >= 2 {
		return matches[1]
	}
	re = regexp.MustCompile(`<button[^>]+name="([^"]*)"[^>]+type="submit"`)
	matches = re.FindStringSubmatch(html)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

func unescHTML(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	return s
}

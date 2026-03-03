package od

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
)

type SPHandler struct {
	Client  *Client
	BaseURL string
}

func parseSPUrl(finalURL string) (baseURL, listPath, rootFolder, id string, err error) {
	u, err := url.Parse(finalURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("invalid URL: %w", err)
	}

	baseURL = fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	id = u.Query().Get("id")
	if id == "" {
		id = u.Query().Get("originalPath")
	}

	if id != "" {
		id, _ = url.PathUnescape(id)
		docIdx := -1
		parts := strings.Split(id, "/")
		for i, p := range parts {
			lower := strings.ToLower(p)
			if lower == "documents" || lower == "shared documents" || lower == "shared%20documents" {
				docIdx = i
				break
			}
		}
		if docIdx >= 0 {
			listPath = strings.Join(parts[:docIdx+1], "/")
			if docIdx+1 < len(parts) {
				rootFolder = strings.Join(parts[docIdx+1:], "/")
			}
		} else {
			listPath = path.Dir(id)
			rootFolder = path.Base(id)
		}
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) >= 2 {
		prefix := strings.ToLower(pathParts[0])
		if prefix == "sites" || prefix == "personal" || prefix == "teams" {
			baseURL = fmt.Sprintf("%s://%s/%s/%s", u.Scheme, u.Host, pathParts[0], pathParts[1])
		}
	}

	return baseURL, listPath, rootFolder, id, nil
}

func (h *SPHandler) ListFiles(finalURL string, body []byte) (*ShareInfo, error) {
	baseURL, listPath, rootFolder, id, err := parseSPUrl(finalURL)
	if err != nil {
		return nil, err
	}
	h.BaseURL = baseURL

	if h.Client.Verbose {
		fmt.Printf("[SharePoint] baseURL=%s listPath=%s rootFolder=%s id=%s\n", baseURL, listPath, rootFolder, id)
	}

	files, err := h.listGraphQL(finalURL, body, listPath, rootFolder, id)
	if err != nil {
		if h.Client.Verbose {
			fmt.Printf("[SharePoint] GraphQL approach failed: %v, trying RenderListDataAsStream\n", err)
		}
		files, err = h.listRender(finalURL, listPath, rootFolder, id)
		if err != nil {
			return nil, fmt.Errorf("failed to list files: %w", err)
		}
	}

	info := &ShareInfo{
		Type:  TypeSP,
		Files: files,
	}
	for _, f := range files {
		info.TotalSize += f.Size
	}
	info.TotalFiles = len(files)
	return info, nil
}

func (h *SPHandler) listGraphQL(finalURL string, pageBody []byte, listPath, rootFolder, id string) ([]FileEntry, error) {
	bodyStr := string(pageBody)

	listID := strBetween(bodyStr, `"listId":"`, `"`)
	if listID == "" {
		listID = strBetween(bodyStr, `listId":"`, `"`)
	}

	listURL := strBetween(bodyStr, `"listUrl":"`, `"`)
	if listURL == "" {
		listURL = strBetween(bodyStr, `listUrl":"`, `"`)
	}

	if listID == "" && listURL == "" {
		return nil, fmt.Errorf("could not extract list info from page")
	}

	if h.Client.Verbose {
		fmt.Printf("[SharePoint] listID=%s listURL=%s\n", listID, listURL)
	}

	folderPath := id
	if folderPath == "" && rootFolder != "" {
		if listPath != "" {
			folderPath = listPath + "/" + rootFolder
		} else {
			folderPath = rootFolder
		}
	}

	return h.listFolder(finalURL, listURL, listID, folderPath, "")
}

func (h *SPHandler) listFolder(referer, listURL, listID, folderPath, prefix string) ([]FileEntry, error) {
	apiURL := h.BaseURL + "/_api/web/GetListUsingPath(DecodedUrl=@a1)/RenderListDataAsStream"

	a1Val := "'" + listURL + "'"
	if listURL == "" {
		a1Val = "'" + folderPath + "'"
	}

	params := url.Values{}
	params.Set("@a1", a1Val)
	params.Set("RootFolder", folderPath)
	params.Set("TryNewExperienceSingle", "TRUE")

	fullURL := apiURL + "?" + params.Encode()
	postBody := `{"parameters":{"RenderOptions":5639,"AllowMultipleValueFilterForTaxonomyFields":true,"AddRequiredFields":true}}`

	resp, err := h.Client.HTTP.R().
		SetHeader("Content-Type", "application/json;odata=verbose").
		SetHeader("Accept", "application/json;odata=verbose").
		SetHeader("Referer", referer).
		SetBody(postBody).
		Post(fullURL)
	if err != nil {
		return nil, fmt.Errorf("RenderListDataAsStream request failed: %w", err)
	}

	if resp.StatusCode() != 200 {
		body := resp.String()
		if len(body) > 500 {
			body = body[:500]
		}
		return nil, fmt.Errorf("RenderListDataAsStream returned %d: %s", resp.StatusCode(), body)
	}

	return h.parseListResp(resp.Body(), referer, listURL, listID, prefix)
}

func (h *SPHandler) parseListResp(respBody []byte, referer, listURL, listID, prefix string) ([]FileEntry, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	listData, ok := result["ListData"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no ListData in response")
	}

	rows, ok := listData["Row"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no Row data in ListData")
	}

	var files []FileEntry
	for _, rowRaw := range rows {
		row, ok := rowRaw.(map[string]interface{})
		if !ok {
			continue
		}

		name := getStr(row, "FileLeafRef")
		if name == "" {
			name = getStr(row, "LinkFilename")
		}
		objType := getStr(row, "FSObjType")

		relPath := name
		if prefix != "" {
			relPath = prefix + "/" + name
		}

		if objType == "1" {
			fp := getStr(row, "FileRef")
			if fp == "" {
				continue
			}
			subFiles, err := h.listFolder(referer, listURL, listID, fp, relPath)
			if err != nil {
				if h.Client.Verbose {
					fmt.Printf("[SharePoint] Warning: failed to list subfolder %s: %v\n", name, err)
				}
				continue
			}
			files = append(files, subFiles...)
		} else {
			size := getInt(row, "FileSizeDisplay")
			if size == 0 {
				size = getInt(row, "File_x0020_Size")
			}

			uid := getStr(row, "UniqueId")
			fileRef := getStr(row, "FileRef")
			dlURL := ""
			if uid != "" {
				uid = strings.Trim(uid, "{}")
				dlURL = h.BaseURL + "/_layouts/15/download.aspx?UniqueId=" + uid
			} else if fileRef != "" {
				dlURL = fmt.Sprintf("%s://%s%s", "https",
					strings.TrimPrefix(strings.TrimPrefix(h.BaseURL, "https://"), "http://"),
					fileRef)
			}

			files = append(files, FileEntry{
				Name:    name,
				Size:    size,
				DlURL:   dlURL,
				RelPath: relPath,
			})
		}
	}

	if nh, ok := listData["NextHref"].(string); ok && nh != "" {
		nextFiles, err := h.nextPage(referer, nh, listURL, listID, prefix)
		if err != nil {
			if h.Client.Verbose {
				fmt.Printf("[SharePoint] Warning: pagination failed: %v\n", err)
			}
		} else {
			files = append(files, nextFiles...)
		}
	}

	return files, nil
}

func (h *SPHandler) nextPage(referer, nextHref, listURL, listID, prefix string) ([]FileEntry, error) {
	apiURL := h.BaseURL + "/_api/web/GetListUsingPath(DecodedUrl=@a1)/RenderListDataAsStream"
	encodedListURL := url.QueryEscape("'" + listURL + "'")
	sep := "&"
	if !strings.Contains(nextHref, "?") {
		sep = "?"
	}
	fullURL := apiURL + nextHref + sep + "@a1=" + encodedListURL
	postBody := `{"parameters":{"RenderOptions":2,"AllowMultipleValueFilterForTaxonomyFields":true,"AddRequiredFields":true}}`

	resp, err := h.Client.HTTP.R().
		SetHeader("Content-Type", "application/json;odata=verbose").
		SetHeader("Accept", "application/json;odata=verbose").
		SetHeader("Referer", referer).
		SetBody(postBody).
		Post(fullURL)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("pagination returned %d", resp.StatusCode())
	}

	return h.parseListResp(resp.Body(), referer, listURL, listID, prefix)
}

func (h *SPHandler) listRender(finalURL, listPath, rootFolder, id string) ([]FileEntry, error) {
	listURL := listPath
	folderPath := id
	if folderPath == "" {
		if rootFolder != "" {
			folderPath = listPath + "/" + rootFolder
		} else {
			folderPath = listPath
		}
	}
	return h.listFolder(finalURL, listURL, "", folderPath, "")
}

func strBetween(s, start, end string) string {
	idx := strings.Index(s, start)
	if idx < 0 {
		return ""
	}
	idx += len(start)
	endIdx := strings.Index(s[idx:], end)
	if endIdx < 0 {
		return ""
	}
	return s[idx : idx+endIdx]
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case string:
			var i int64
			fmt.Sscanf(n, "%d", &i)
			return i
		}
	}
	return 0
}

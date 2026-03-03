package od

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	appID    = "5cbed6ac-a083-4e14-b191-b4ba07653de2"
	tokenURL = "https://api-badgerp.svc.ms/v1.0/token"
	apiBase  = "https://my.microsoftpersonalcontent.com"
)

type PersonalHandler struct {
	Client     *Client
	authScheme string
	authToken  string
}

type tokenResp struct {
	AuthScheme string `json:"authScheme"`
	Token      string `json:"token"`
}

type driveItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	DlURL   string `json:"@content.downloadUrl"`
	Folder  *struct {
		ChildCount int `json:"childCount"`
	} `json:"folder"`
	ParentRef struct {
		DriveID string `json:"driveId"`
		ID      string `json:"id"`
		Path    string `json:"path"`
	} `json:"parentReference"`
}

type childrenResp struct {
	Value    []driveItem `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

func (h *PersonalHandler) ListFiles(finalURL string, body []byte) (*ShareInfo, error) {
	redeem, err := getRedeem(finalURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract redeem token: %w", err)
	}

	if h.Client.Verbose {
		fmt.Printf("[Personal] redeemToken=%s\n", redeem)
	}

	if err := h.getToken(); err != nil {
		return nil, fmt.Errorf("failed to acquire token: %w", err)
	}

	driveID, itemID, rootItem, err := h.redeem(redeem)
	if err != nil {
		return nil, fmt.Errorf("failed to redeem share: %w", err)
	}

	if h.Client.Verbose {
		fmt.Printf("[Personal] driveID=%s itemID=%s\n", driveID, itemID)
	}

	var files []FileEntry

	if rootItem != nil && rootItem.Folder == nil {
		files = append(files, FileEntry{
			Name:    rootItem.Name,
			Size:    rootItem.Size,
			DlURL:   rootItem.DlURL,
			RelPath: rootItem.Name,
		})
	} else {
		files, err = h.listKids(driveID, itemID, "")
		if err != nil {
			return nil, fmt.Errorf("failed to list children: %w", err)
		}
	}

	info := &ShareInfo{
		Type:  TypePersonal,
		Files: files,
	}
	for _, f := range files {
		info.TotalSize += f.Size
	}
	info.TotalFiles = len(files)
	return info, nil
}

func getRedeem(finalURL string) (string, error) {
	u, err := url.Parse(finalURL)
	if err != nil {
		return "", err
	}

	if r := u.Query().Get("redeem"); r != "" {
		return r, nil
	}
	if e := u.Query().Get("e"); e != "" {
		return e, nil
	}

	resid := u.Query().Get("resid")
	authkey := u.Query().Get("authkey")
	if resid != "" && authkey != "" {
		shareURL := fmt.Sprintf("https://onedrive.live.com/?resid=%s&authkey=%s", resid, authkey)
		return encodeShare(shareURL), nil
	}

	return encodeShare(finalURL), nil
}

func encodeShare(rawURL string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(rawURL))
	enc = strings.TrimRight(enc, "=")
	enc = strings.ReplaceAll(enc, "/", "_")
	enc = strings.ReplaceAll(enc, "+", "-")
	return "u!" + enc
}

func (h *PersonalHandler) getToken() error {
	resp, err := h.Client.HTTP.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetBody(fmt.Sprintf(`{"appId":"%s"}`, appID)).
		Post(tokenURL)
	if err != nil {
		return err
	}

	if resp.StatusCode() != 200 {
		body := resp.String()
		if len(body) > 500 {
			body = body[:500]
		}
		return fmt.Errorf("token request failed with %d: %s", resp.StatusCode(), body)
	}

	var tr tokenResp
	if err := json.Unmarshal(resp.Body(), &tr); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	h.authScheme = tr.AuthScheme
	h.authToken = tr.Token
	return nil
}

func (h *PersonalHandler) redeem(redeemToken string) (driveID, itemID string, item *driveItem, err error) {
	shareURL := fmt.Sprintf("%s/_api/v2.0/shares/%s/driveitem", apiBase, redeemToken)

	resp, err := h.Client.HTTP.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("Prefer", "autoredeem").
		SetHeader("Authorization", fmt.Sprintf("%s %s", h.authScheme, h.authToken)).
		SetBody("{}").
		Post(shareURL)
	if err != nil {
		return "", "", nil, err
	}

	if resp.StatusCode() != 200 {
		body := resp.String()
		if len(body) > 500 {
			body = body[:500]
		}
		return "", "", nil, fmt.Errorf("redeem failed with %d: %s", resp.StatusCode(), body)
	}

	var di driveItem
	if err := json.Unmarshal(resp.Body(), &di); err != nil {
		return "", "", nil, fmt.Errorf("failed to parse redeem response: %w", err)
	}

	return di.ParentRef.DriveID, di.ID, &di, nil
}

func (h *PersonalHandler) listKids(driveID, itemID, prefix string) ([]FileEntry, error) {
	reqURL := fmt.Sprintf("%s/_api/v2.0/drives/%s/items/%s/children", apiBase, driveID, itemID)

	var allFiles []FileEntry
	nextLink := reqURL

	for nextLink != "" {
		items, next, err := h.fetchKids(nextLink)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			relPath := item.Name
			if prefix != "" {
				relPath = prefix + "/" + item.Name
			}

			if item.Folder != nil {
				subFiles, err := h.listKids(driveID, item.ID, relPath)
				if err != nil {
					if h.Client.Verbose {
						fmt.Printf("[Personal] Warning: failed to list subfolder %s: %v\n", item.Name, err)
					}
					continue
				}
				allFiles = append(allFiles, subFiles...)
			} else {
				allFiles = append(allFiles, FileEntry{
					Name:    item.Name,
					Size:    item.Size,
					DlURL:   item.DlURL,
					RelPath: relPath,
				})
			}
		}

		nextLink = next
	}

	return allFiles, nil
}

func (h *PersonalHandler) fetchKids(reqURL string) ([]driveItem, string, error) {
	resp, err := h.Client.HTTP.R().
		SetHeader("Accept", "application/json").
		SetHeader("Authorization", fmt.Sprintf("%s %s", h.authScheme, h.authToken)).
		Get(reqURL)
	if err != nil {
		return nil, "", err
	}

	if resp.StatusCode() != 200 {
		body := resp.String()
		if len(body) > 500 {
			body = body[:500]
		}
		return nil, "", fmt.Errorf("children request returned %d: %s", resp.StatusCode(), body)
	}

	var result childrenResp
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, "", fmt.Errorf("failed to parse children response: %w", err)
	}

	return result.Value, result.NextLink, nil
}

package od

import (
	"net/http/cookiejar"

	"github.com/go-resty/resty/v2"
)

const defaultUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"

type Client struct {
	HTTP    *resty.Client
	Verbose bool
}

func NewClient(verbose bool) *Client {
	jar, _ := cookiejar.New(nil)
	r := resty.New().
		SetCookieJar(jar).
		SetHeader("User-Agent", defaultUA).
		SetHeader("Accept-Language", "zh-CN,en;q=0.9").
		SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if verbose {
		r.SetDebug(true)
	}
	return &Client{HTTP: r, Verbose: verbose}
}

func (c *Client) ResolveURL(rawURL string) (string, []byte, error) {
	resp, err := c.HTTP.R().Get(rawURL)
	if err != nil {
		return "", nil, err
	}
	return resp.RawResponse.Request.URL.String(), resp.Body(), nil
}
